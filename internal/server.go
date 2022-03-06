// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"git.mills.io/prologic/observe"
	"github.com/NYTimes/gziphandler"
	humanize "github.com/dustin/go-humanize"
	"github.com/gabstv/merger"
	"github.com/justinas/nosurf"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
	metricsMiddlewarePrometheus "github.com/slok/go-http-metrics/metrics/prometheus"
	metricsMiddleware "github.com/slok/go-http-metrics/middleware"
	httproutermiddleware "github.com/slok/go-http-metrics/middleware/httprouter"
	"github.com/unrolled/logger"
	"golang.org/x/crypto/acme/autocert"
	"willnorris.com/go/microformats"

	"git.mills.io/yarnsocial/yarn"
	"git.mills.io/yarnsocial/yarn/internal/auth"
	"git.mills.io/yarnsocial/yarn/internal/indieweb"
	"git.mills.io/yarnsocial/yarn/internal/passwords"
	"git.mills.io/yarnsocial/yarn/internal/session"
	"go.yarn.social/types"
)

const (
	acmeDir = "acme"
)

var (
	metrics     *observe.Metrics
	webmentions *indieweb.WebMention
	websub      *indieweb.WebSub

	//go:embed theme
	builtinThemeFS embed.FS
)

func init() {
	metrics = observe.NewMetrics("yarnd")
}

// Server ...
type Server struct {
	bind    string
	config  *Config
	tmplman *TemplateManager
	router  *Router
	server  *http.Server

	// Feed Cache
	cache *Cache

	// Feed Archiver
	archive Archiver

	// Data Store
	db Store

	// Scheduler
	cron *cron.Cron

	// Dispatcher
	tasks *Dispatcher

	// Auth
	am *auth.Manager

	// Sessions
	sc *SessionStore
	sm *session.Manager

	// API
	api *API

	// Passwords
	pm passwords.Passwords

	// Translator
	translator *Translator

	// Factory Functions
	AppendTwt  AppendTwtFunc
	FilterTwts FilterTwtsFunc
}

func (s *Server) render(name string, w http.ResponseWriter, ctx *Context) {
	//
	// Update timeline view(s) UpdatedAt timestamps
	//
	// TODO: Refactor Context/Render to have pre/post hooks?

	if ctx.User.IsZero() {
		if ctx.DiscoverUpdatedAt.IsZero() {
			ctx.DiscoverUpdatedAt = s.discoverUpdatedAt(ctx.User)
		}
		ctx.TimelineUpdatedAt = ctx.DiscoverUpdatedAt
	} else {
		if ctx.TimelineUpdatedAt.IsZero() {
			ctx.TimelineUpdatedAt = s.timelineUpdatedAt(ctx.User)
		}
		if ctx.DiscoverUpdatedAt.IsZero() {
			ctx.DiscoverUpdatedAt = s.discoverUpdatedAt(ctx.User)
		}
		if ctx.LastMentionedAt.IsZero() {
			ctx.LastMentionedAt = s.lastMentionedAt(ctx.User)
		}
	}

	ctx.View = name

	buf, err := s.tmplman.Exec(name, ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = buf.WriteTo(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// AddRouter ...
func (s *Server) AddRoute(method, path string, handler http.Handler) {
	s.router.Handler(method, path, handler)
}

// AddShutdownHook ...
func (s *Server) AddShutdownHook(f func()) {
	s.server.RegisterOnShutdown(f)
}

// Shutdown ...
func (s *Server) Shutdown(ctx context.Context) error {
	s.cron.Stop()
	s.tasks.Stop()

	if err := s.server.Shutdown(ctx); err != nil {
		log.WithError(err).Error("error shutting down server")
		return err
	}

	if err := s.db.Close(); err != nil {
		log.WithError(err).Error("error closing store")
		return err
	}

	return nil
}

// Run ...
func (s *Server) Run() (err error) {
	idleConnsClosed := make(chan struct{})

	go func() {
		if err = s.ListenAndServe(); err != http.ErrServerClosed {
			// Error starting or closing listener:
			log.WithError(err).Fatal("HTTP server ListenAndServe")
		}
	}()

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigch
	log.Infof("Received signal %s", sig)

	log.Info("Shutting down...")

	// We received an interrupt signal, shut down.
	if err = s.Shutdown(context.Background()); err != nil {
		// Error from closing listeners, or context timeout:
		log.WithError(err).Fatal("Error shutting down HTTP server")
	}
	close(idleConnsClosed)

	<-idleConnsClosed

	return
}

// ListenAndServe ...
func (s *Server) ListenAndServe() error {
	_, port, err := net.SplitHostPort(s.bind)
	if err != nil {
		log.WithError(err).Errorf("error parsing bind hostport %s", s.bind)
		return err
	}

	useLetsEncrypt := s.config.TLSKey == "" && s.config.TLSCert == ""

	if s.config.TLS {
		if useLetsEncrypt && (port == "443" || port == "https") {
			log.Info("Setting up Lets Encrypt ...")

			m := &autocert.Manager{
				Cache:      autocert.DirCache(filepath.Join(s.config.Data, acmeDir)),
				Prompt:     autocert.AcceptTOS,
				Email:      s.config.AdminEmail,
				HostPolicy: autocert.HostWhitelist(s.config.baseURL.Hostname()),
			}
			s.server.TLSConfig = m.TLSConfig()

			httpServer := &http.Server{
				Addr: ":http",
				Handler: logger.New(logger.Options{
					Prefix:               "yarnd-http",
					RemoteAddressHeaders: []string{"X-Forwarded-For"},
				}).Handler(m.HTTPHandler(nil)),
			}

			go func() {
				if err := httpServer.ListenAndServe(); err != nil {
					log.WithError(err).Fatalf("error running http server")
				}
			}()

			return s.server.ListenAndServeTLS("", "")
		}
		log.Infof("Setting up TLS (key=%s cert=%s)", s.config.TLSKey, s.config.TLSCert)
		return s.server.ListenAndServeTLS(s.config.TLSCert, s.config.TLSKey)
	}
	log.Warn("No TLS configured")
	return s.server.ListenAndServe()
}

// AddCronJob ...
func (s *Server) AddCronJob(spec string, job cron.Job) error {
	return s.cron.AddJob(spec, job)
}

func (s *Server) setupMetrics() {
	ctime := time.Now()

	// server uptime counter
	metrics.NewCounterFunc(
		"server", "uptime",
		"Number of nanoseconds the server has been running",
		func() float64 {
			return float64(time.Since(ctime).Nanoseconds())
		},
	)

	// sessions
	metrics.NewGaugeFunc(
		"server", "sessions",
		"Number of active in-memory sessions (non-persistent)",
		func() float64 {
			return float64(s.sc.Count())
		},
	)

	// dau (daily active users)
	metrics.NewGauge(
		"server", "dau",
		"Number of daily active users",
	)

	// mau (monthly active users)
	metrics.NewGauge(
		"server", "mau",
		"Number of monthly active users",
	)

	// database keys
	metrics.NewGaugeFunc(
		"db", "feeds",
		"Number of database /feeds keys",
		func() float64 {
			return float64(s.db.LenFeeds())
		},
	)
	metrics.NewGaugeFunc(
		"db", "sessions",
		"Number of database /sessions keys",
		func() float64 {
			return float64(s.db.LenSessions())
		},
	)
	metrics.NewGaugeFunc(
		"db", "users",
		"Number of database /users keys",
		func() float64 {
			return float64(s.db.LenUsers())
		},
	)

	// feed cache size
	metrics.NewGaugeFunc(
		"cache", "feeds",
		"Number of unique feeds in the global feed cache",
		func() float64 {
			return float64(s.cache.FeedCount())
		},
	)

	// feed cache size
	metrics.NewGaugeFunc(
		"cache", "twts",
		"Number of active twts in the global feed cache",
		func() float64 {
			return float64(s.cache.TwtCount())
		},
	)

	// feed cache processing time
	metrics.NewGauge(
		"cache", "last_processed_seconds",
		"Number of seconds for a feed cache cycle",
	)

	// feed cache convergence time
	metrics.NewGauge(
		"cache", "last_convergence_seconds",
		"Number of seconds for cache convergence",
	)

	// feed cache limited fetch (feed exceeded MaxFetchLImit or unknown size)
	metrics.NewCounter(
		"cache", "limited",
		"Number of feed cache fetches affected by MaxFetchLimit",
	)

	// no. of missing twts found in feed cache
	metrics.NewCounter(
		"cache", "missing_twts",
		"Number of missing twts found in the feed cache",
	)

	// archive size
	metrics.NewCounter(
		"archive", "size",
		"Number of items inserted into the global feed archive",
	)

	// archive errors
	metrics.NewCounter(
		"archive", "error",
		"Number of items errored inserting into the global feed archive",
	)

	// server info
	metrics.NewGaugeVec(
		"server", "info",
		"Server information",
		[]string{"full_version", "version", "commit"},
	)
	metrics.GaugeVec("server", "info").
		With(map[string]string{
			"full_version": yarn.FullVersion(),
			"version":      yarn.Version,
			"commit":       yarn.Commit,
		}).Set(1)

	// old avatars
	metrics.NewCounter(
		"media", "old_avatar",
		"Count of old Avtar (PNG) conversions",
	)
	// old media
	metrics.NewCounter(
		"media", "old_media",
		"Count of old Media (PNG) served",
	)

	s.AddRoute("GET", "/metrics", metrics.Handler())
}

func (s *Server) processWebMention(source, target *url.URL, data *microformats.Data) error {
	log.
		WithField("source", source).
		WithField("target", target).
		Debugf("received webmention from %s to %s", source.String(), target.String())

	getEntry := func(data *microformats.Data) (*microformats.Microformat, error) {
		if data != nil {
			for _, item := range data.Items {
				if HasString(item.Type, "h-entry") {
					return item, nil
				}
			}
		}
		return nil, errors.New("error: no entry found")
	}

	getAuthor := func(entry *microformats.Microformat) (*microformats.Microformat, error) {
		if entry != nil {
			authors := entry.Properties["author"]
			if len(authors) > 0 {
				if v, ok := authors[0].(*microformats.Microformat); ok {
					return v, nil
				}
			}
		}
		return nil, errors.New("error: no author found")
	}

	processData := func(data *microformats.Data) (name, summary, feed string, err error) {
		if data == nil {
			return
		}

		entry, err := getEntry(data)
		if err != nil {
			log.WithError(err).Error("error getting entry")
			return
		}

		if summaries := entry.Properties["summary"]; len(summaries) > 0 {
			if v, ok := summaries[0].(string); ok {
				summary = strings.TrimSpace(v)
			}
		}

		author, err := getAuthor(entry)
		if err != nil {
			log.WithError(err).Error("error getting author")
			return
		}

		if names := author.Properties["name"]; len(names) > 0 {
			if v, ok := names[0].(string); ok {
				name = strings.TrimSpace(v)
			}
		}

		for url, rel := range data.RelURLs {
			if rel.Type == "text/plain" {
				feed = url
				break
			}
		}

		return
	}

	var (
		user      *User
		userError error
	)

	if strings.HasPrefix(target.Path, "/twt/") {
		hash := strings.TrimPrefix(target.Path, "/twt/")
		if len(hash) < types.TwtHashLength {
			log.Errorf("invalid twt %s from webmention target %s", hash, target.String())
			return fmt.Errorf("invalid twt %s from webmention target %s", hash, target.String())
		}

		bs, err := DecodeHash(hash)
		if err != nil || len(bs) < 2 {
			log.WithError(err).Errorf("error decoding twt %s from webmention target %s", hash, target.String())
			return fmt.Errorf("error decoding twt %s from webmention target %s", hash, target.String())
		}

		twt, inCache := s.cache.Lookup(hash)
		if !inCache {
			// If the twt is not in the cache look for it in the archive
			if s.archive.Has(hash) {
				twt, err = s.archive.Get(hash)
				if err != nil {
					log.WithError(err).Errorf("error loading twt %s from archive", hash)
					return fmt.Errorf("error loading twt %s from archive", hash)
				}
			}
		}

		if twt == nil || twt.IsZero() {
			log.Errorf("invalid twt %s processing webmention from %s", hash, target.String())
			return fmt.Errorf("invalid twt %s processing webmention from %s", hash, target.String())
		}

		user, userError = GetUserFromTwter(s.config, s.db, twt.Twter())
	} else if strings.HasPrefix(target.Path, "/user/") {
		user, userError = GetUserFromURL(s.config, s.db, target.String())
	} else {
		log.Errorf("unable to process webmention from %s", target.String())
		return fmt.Errorf("unable to process webmention from %s", target.String())
	}
	if userError != nil {
		log.WithError(userError).Errorf("error loading user while processing webmention from %s", target.String())
		return fmt.Errorf("error loading user while processing webmention from %s", target.String())
	}

	name, summary, feed, err := processData(data)
	if err != nil {
		log.WithError(err).Warnf("error processing mf2 data from %s", source)
		return err
	}
	log.Debugf("name: %q", name)
	log.Debugf("summary: %q", summary)
	log.Debugf("feed: %q", feed)

	if feed == "" {
		adminUser, err := s.db.GetUser(s.config.AdminUser)
		if err != nil {
			log.WithError(err).Warn("error loading admin user object")
			return err
		}

		supportFeed, err := s.db.GetFeed(supportSpecialUser)
		if err != nil {
			log.WithError(err).Warn("error loading support feed object")
			return err
		}

		// TODO: Make this configurable?
		mentionText := fmt.Sprintf(
			"👋 Hello @<%s %s>, you were web mentioned on %s",
			user.Username, s.config.URLForUser(user.Username),
			source.String(),
		)

		mentionText += fmt.Sprintf("\n\n%s", strings.TrimSpace(Indent(summary, "> ")))

		mentionText = CleanTwt(mentionText)

		mentionTwt, err := s.AppendTwt(adminUser, supportFeed, mentionText)
		if err != nil {
			log.WithError(err).Warnf("error posting mention for %s", user.Username)
			return err
		}
		s.cache.InjectFeed(s.config.URLForUser(supportFeed.Name), mentionTwt)
		if user.Follows(s.config.URLForUser(supportFeed.Name)) {
			s.cache.DeleteUserViews(user)
		}
		return nil
	}

	// If the mention is an ordinary WebMention (no Source Feed)
	// then inject a message notifying the user of the mention via
	// @support feed.
	// Otherwise if the mention came from a Yarn.social Pod (valid Source Feed)
	// AND if the user doesn't already follow the feed and would see the
	// mention normally, then fetch the feed as a once-off (on demand).
	if !user.Follows(feed) {
		sources := make(types.FetchFeedRequests)
		sources[types.FetchFeedRequest{Nick: name, URL: feed}] = true
		s.cache.FetchFeeds(s.config, s.archive, sources, nil)
	}

	return nil
}

func (s *Server) setupWebMentions() {
	webmentions = indieweb.NewWebMention()
	webmentions.Mention = s.processWebMention
}

func (s *Server) processNotification(topic string) error {
	log.Debugf("received notification for %s", topic)

	sources := make(types.FetchFeedRequests)
	sources[types.FetchFeedRequest{Force: true, URL: topic}] = true
	s.cache.FetchFeeds(s.config, s.archive, sources, nil)

	return nil
}

func (s *Server) setupWebSub() {
	websub = indieweb.NewWebSub(fmt.Sprintf("%s/websub", s.config.BaseURL))
	websub.Notify = s.processNotification
}

func (s *Server) setupJobs() error {
	InitJobs(s.config)
	for name, jobSpec := range Jobs {
		if jobSpec.Schedule == "" {
			continue
		}

		job := jobSpec.Factory(s.config, s.cache, s.archive, s.db)
		if err := s.cron.AddJob(jobSpec.Schedule, job); err != nil {
			return fmt.Errorf("invalid cron schedule for job %s: %v (see https://pkg.go.dev/github.com/robfig/cron)", name, err)
		}
		log.Infof("Started background job %s (%s)", name, jobSpec.Schedule)
	}

	return nil
}

func (s *Server) runStartupJobs() {
	Jobs["ActiveUsers"].Factory(s.config, s.cache, s.archive, s.db).Run()

	time.Sleep(time.Second * 5)

	log.Info("running startup jobs")
	for name, jobSpec := range StartupJobs {
		job := jobSpec.Factory(s.config, s.cache, s.archive, s.db)
		log.Infof("running %s now...", name)
		job.Run()
	}

	// Merge store
	if err := s.db.Merge(); err != nil {
		log.WithError(err).Error("error merging store")
	}
}

func (s *Server) initRoutes() {
	var (
		staticDir string
		staticFS  fs.FS
		err       error
	)

	if s.config.Theme == "" {
		staticDir = "./internal/theme/static"
		staticFS, err = fs.Sub(builtinThemeFS, "theme/static")
		if err != nil {
			log.WithError(err).Fatalf("error loading builtin theme static assets")
		}
	} else {
		staticDir = filepath.Join(s.config.Theme, "static")
		staticFS = os.DirFS(staticDir)
	}

	// To serve up artbitrary static assets in /path/to/theme/static/custom/...
	s.router.Static("/custom/*filepath", filepath.Join(staticDir, "custom"))

	if s.config.Debug {
		s.router.ServeFiles("/css/*filepath", http.Dir(filepath.Join(staticDir, "css")))
		s.router.ServeFiles("/img/*filepath", http.Dir(filepath.Join(staticDir, "img")))
		s.router.ServeFiles("/js/*filepath", http.Dir(filepath.Join(staticDir, "js")))
	} else {
		cssFS, err := fs.Sub(staticFS, "css")
		if err != nil {
			log.Fatal("error getting SubFS for static/css")
		}

		jsFS, err := fs.Sub(staticFS, "js")
		if err != nil {
			log.Fatal("error getting SubFS for static/js")
		}

		imgFS, err := fs.Sub(staticFS, "img")
		if err != nil {
			log.Fatal("error getting SubFS for static/img")
		}

		s.router.ServeFilesWithCacheControl("/css/:commit/*filepath", cssFS)
		s.router.ServeFilesWithCacheControl("/img/:commit/*filepath", imgFS)
		s.router.ServeFilesWithCacheControl("/js/:commit/*filepath", jsFS)
	}

	mdlw := metricsMiddleware.New(metricsMiddleware.Config{
		Recorder: metricsMiddlewarePrometheus.NewRecorder(
			metricsMiddlewarePrometheus.Config{
				Prefix: "yarnd",
			},
		),
		Service:       "yarnd",
		GroupedStatus: true,
	})

	s.router.NotFound = http.HandlerFunc(s.NotFoundHandler)

	s.router.GET("/about", httproutermiddleware.Handler("page", s.PageHandler("about"), mdlw))
	s.router.GET("/help", httproutermiddleware.Handler("page", s.PageHandler("help"), mdlw))
	s.router.GET("/privacy", httproutermiddleware.Handler("page", s.PageHandler("privacy"), mdlw))
	s.router.GET("/abuse", httproutermiddleware.Handler("page", s.PageHandler("abuse"), mdlw))

	s.router.GET("/", httproutermiddleware.Handler("timeline", s.TimelineHandler(), mdlw))
	s.router.HEAD("/", httproutermiddleware.Handler("timeline", s.TimelineHandler(), mdlw))

	s.router.GET("/robots.txt", httproutermiddleware.Handler("robots", s.RobotsHandler(), mdlw))
	s.router.HEAD("/robots.txt", httproutermiddleware.Handler("robots", s.RobotsHandler(), mdlw))

	s.router.GET("/discover", httproutermiddleware.Handler("discover", s.am.MustAuth(s.DiscoverHandler()), mdlw))
	s.router.GET("/mentions", httproutermiddleware.Handler("mentions", s.am.MustAuth(s.MentionsHandler()), mdlw))
	s.router.GET("/search", httproutermiddleware.Handler("search", s.SearchHandler(), mdlw))

	s.router.HEAD("/twt/:hash", httproutermiddleware.Handler("twt", s.PermalinkHandler(), mdlw))
	s.router.GET("/twt/:hash", httproutermiddleware.Handler("twt", s.PermalinkHandler(), mdlw))

	s.router.GET("/bookmark/:hash", httproutermiddleware.Handler("bookmark", s.am.MustAuth(s.BookmarkHandler()), mdlw))
	s.router.POST("/bookmark/:hash", httproutermiddleware.Handler("bookmark", s.am.MustAuth(s.BookmarkHandler()), mdlw))

	s.router.HEAD("/conv/:hash", httproutermiddleware.Handler("conv", s.ConversationHandler(), mdlw))
	s.router.GET("/conv/:hash", httproutermiddleware.Handler("conv", s.ConversationHandler(), mdlw))

	s.router.GET("/feeds", httproutermiddleware.Handler("feeds", s.am.MustAuth(s.FeedsHandler()), mdlw))
	s.router.POST("/feed", httproutermiddleware.Handler("feeds", s.am.MustAuth(s.FeedHandler()), mdlw))

	s.router.POST("/post", httproutermiddleware.Handler("post", s.am.MustAuth(s.PostHandler()), mdlw))
	s.router.PATCH("/post", httproutermiddleware.Handler("post", s.am.MustAuth(s.PostHandler()), mdlw))
	s.router.DELETE("/post", httproutermiddleware.Handler("post", s.am.MustAuth(s.PostHandler()), mdlw))

	// TODO: Figure out how to internally rewrite/proxy /~:nick -> /user/:nick

	// XXX: HEAD is always exposed for IndieAuth Authorization Discovery
	s.router.HEAD("/user/:nick", s.ProfileHandler())

	if s.config.OpenProfiles {
		s.router.GET("/user/:nick/", httproutermiddleware.Handler("user", s.ProfileHandler(), mdlw))
		s.router.GET("/user/:nick/config.yaml", httproutermiddleware.Handler("user_config", s.UserConfigHandler(), mdlw))
	} else {
		s.router.GET("/user/:nick/", httproutermiddleware.Handler("user", s.am.MustAuth(s.ProfileHandler()), mdlw))
		s.router.GET("/user/:nick/config.yaml", httproutermiddleware.Handler("user_config", s.am.MustAuth(s.UserConfigHandler()), mdlw))
	}
	s.router.GET("/user/:nick/avatar", httproutermiddleware.Handler("avatar", s.AvatarHandler(), mdlw))
	s.router.HEAD("/user/:nick/avatar", httproutermiddleware.Handler("avatar", s.AvatarHandler(), mdlw))
	s.router.HEAD("/user/:nick/twtxt.txt", httproutermiddleware.Handler("twtxt", s.TwtxtHandler(), mdlw))
	s.router.GET("/user/:nick/twtxt.txt", httproutermiddleware.Handler("twtxt", s.TwtxtHandler(), mdlw))
	s.router.GET("/user/:nick/followers", httproutermiddleware.Handler("followers", s.FollowersHandler(), mdlw))
	s.router.GET("/user/:nick/following", httproutermiddleware.Handler("following", s.FollowingHandler(), mdlw))
	s.router.GET("/user/:nick/bookmarks", httproutermiddleware.Handler("bookmarks", s.BookmarksHandler(), mdlw))

	// WebMentions
	s.router.POST("/webmention", httproutermiddleware.Handler("webmentions", s.WebMentionHandler(), mdlw))

	// WebSub
	s.router.GET("/websub", httproutermiddleware.Handler("websub", s.WebSubHandler(), mdlw))
	s.router.POST("/websub", httproutermiddleware.Handler("websub", s.WebSubHandler(), mdlw))
	s.router.GET("/notify", httproutermiddleware.Handler("notify", s.NotifyHandler(), mdlw))
	s.router.POST("/notify", httproutermiddleware.Handler("notify", s.NotifyHandler(), mdlw))

	// Syndication Formats (RSS, Atom, JSON Feed)
	s.router.HEAD("/user/:nick/atom.xml", httproutermiddleware.Handler("user_atom", s.SyndicationHandler(), mdlw))
	s.router.GET("/user/:nick/atom.xml", httproutermiddleware.Handler("user_atom", s.SyndicationHandler(), mdlw))

	if s.config.OpenProfiles {
		s.router.GET("/~:nick/", httproutermiddleware.Handler("user", s.ProfileHandler(), mdlw))
		s.router.GET("/~:nick/config.yaml", httproutermiddleware.Handler("user_config", s.UserConfigHandler(), mdlw))
	} else {
		s.router.GET("/~:nick/", httproutermiddleware.Handler("user", s.am.MustAuth(s.ProfileHandler()), mdlw))
		s.router.GET("/~:nick/config.yaml", httproutermiddleware.Handler("user_config", s.am.MustAuth(s.UserConfigHandler()), mdlw))
	}
	s.router.GET("/~:nick/avatar", httproutermiddleware.Handler("avatar", s.AvatarHandler(), mdlw))
	s.router.HEAD("/~:nick/avatar", httproutermiddleware.Handler("avatar", s.AvatarHandler(), mdlw))
	s.router.HEAD("/~:nick/twtxt.txt", httproutermiddleware.Handler("twtxt", s.TwtxtHandler(), mdlw))
	s.router.GET("/~:nick/twtxt.txt", httproutermiddleware.Handler("twtxt", s.TwtxtHandler(), mdlw))
	s.router.GET("/~:nick/followers", httproutermiddleware.Handler("followers", s.FollowersHandler(), mdlw))
	s.router.GET("/~:nick/following", httproutermiddleware.Handler("following", s.FollowingHandler(), mdlw))
	s.router.GET("/~:nick/bookmarks", httproutermiddleware.Handler("bookmarks", s.BookmarksHandler(), mdlw))

	// Syndication Formats (RSS, Atom, JSON Feed)
	s.router.HEAD("/~:nick/atom.xml", httproutermiddleware.Handler("user_atom", s.SyndicationHandler(), mdlw))
	s.router.GET("/~:nick/atom.xml", httproutermiddleware.Handler("user_atom", s.SyndicationHandler(), mdlw))

	// IndieAuth  Authorization Endpoint
	s.router.GET("/indieauth/auth", httproutermiddleware.Handler("indieauth_auth", s.am.MustAuth(s.IndieAuthHandler()), mdlw))
	s.router.POST("/indieauth/auth", httproutermiddleware.Handler("indieauth_verify", s.IndieAuthVerifyHandler(), mdlw))
	s.router.GET("/indieauth/callback", httproutermiddleware.Handler("indieauth_callback", s.am.MustAuth(s.IndieAuthCallbackHandler()), mdlw))

	// External Feeds
	s.router.GET("/external", httproutermiddleware.Handler("external", s.ExternalHandler(), mdlw))
	s.router.GET("/externalFollowing", httproutermiddleware.Handler("external_following", s.ExternalFollowingHandler(), mdlw))
	s.router.GET("/externalAvatar", httproutermiddleware.Handler("external_avatar", s.ExternalAvatarHandler(), mdlw))
	s.router.HEAD("/externalAvatar", httproutermiddleware.Handler("external_avatar", s.ExternalAvatarHandler(), mdlw))

	// External Queries (protected by a short-lived token)
	s.router.GET("/whoFollows", httproutermiddleware.Handler("whoFollows", s.WhoFollowsHandler(), mdlw))

	// Syndication Formats (RSS, Atom, JSON Feed)
	s.router.HEAD("/atom.xml", httproutermiddleware.Handler("atom", s.SyndicationHandler(), mdlw))
	s.router.GET("/atom.xml", httproutermiddleware.Handler("atom", s.SyndicationHandler(), mdlw))

	s.router.GET("/feed/:name/manage", httproutermiddleware.Handler("feed_manage", s.am.MustAuth(s.ManageFeedHandler()), mdlw))
	s.router.POST("/feed/:name/manage", httproutermiddleware.Handler("feed_manage", s.am.MustAuth(s.ManageFeedHandler()), mdlw))
	s.router.POST("/feed/:name/delete", httproutermiddleware.Handler("feed_delete", s.am.MustAuth(s.DeleteFeedHandler()), mdlw))

	s.router.GET("/login", httproutermiddleware.Handler("login", s.am.HasAuth(s.LoginHandler()), mdlw))
	s.router.POST("/login", httproutermiddleware.Handler("login", s.LoginHandler(), mdlw))

	s.router.GET("/login/email", httproutermiddleware.Handler("login_email", s.am.HasAuth(s.LoginEmailHandler()), mdlw))
	s.router.POST("/login/email", httproutermiddleware.Handler("login_email", s.LoginEmailHandler(), mdlw))
	s.router.GET("/magiclinkauth", httproutermiddleware.Handler("magiclinkauth", s.MagicLinkAuthHandler(), mdlw))

	s.router.GET("/logout", httproutermiddleware.Handler("logout", s.LogoutHandler(), mdlw))
	s.router.POST("/logout", httproutermiddleware.Handler("logout", s.LogoutHandler(), mdlw))

	s.router.GET("/register", httproutermiddleware.Handler("register", s.am.HasAuth(s.RegisterHandler()), mdlw))
	s.router.POST("/register", httproutermiddleware.Handler("register", s.RegisterHandler(), mdlw))

	// Reset Password
	s.router.GET("/resetPassword", httproutermiddleware.Handler("resetPassword", s.ResetPasswordHandler(), mdlw))
	s.router.POST("/resetPassword", httproutermiddleware.Handler("resetPassword", s.ResetPasswordHandler(), mdlw))
	s.router.GET("/newPassword", httproutermiddleware.Handler("resetPassword", s.ResetPasswordMagicLinkHandler(), mdlw))
	s.router.POST("/newPassword", httproutermiddleware.Handler("newPassword", s.NewPasswordHandler(), mdlw))

	// Media Handling
	s.router.GET("/media/:name", httproutermiddleware.Handler("media", s.MediaHandler(), mdlw))
	s.router.HEAD("/media/:name", httproutermiddleware.Handler("media", s.MediaHandler(), mdlw))
	s.router.POST("/upload", httproutermiddleware.Handler("upload", s.am.MustAuth(s.UploadMediaHandler()), mdlw))

	// Task State
	s.router.GET("/task/:uuid", httproutermiddleware.Handler("task", s.TaskHandler(), mdlw))

	// User/Feed Lookups
	s.router.GET("/lookup", httproutermiddleware.Handler("lookup", s.am.MustAuth(s.LookupHandler()), mdlw))

	s.router.GET("/follow", httproutermiddleware.Handler("follow", s.am.MustAuth(s.FollowHandler()), mdlw))
	s.router.POST("/follow", httproutermiddleware.Handler("follow", s.am.MustAuth(s.FollowHandler()), mdlw))

	s.router.GET("/import", httproutermiddleware.Handler("import", s.am.MustAuth(s.ImportHandler()), mdlw))
	s.router.POST("/import", httproutermiddleware.Handler("import", s.am.MustAuth(s.ImportHandler()), mdlw))

	s.router.GET("/unfollow", httproutermiddleware.Handler("unfollow", s.am.MustAuth(s.UnfollowHandler()), mdlw))
	s.router.POST("/unfollow", httproutermiddleware.Handler("unfollow", s.am.MustAuth(s.UnfollowHandler()), mdlw))

	s.router.GET("/mute", httproutermiddleware.Handler("mute", s.am.MustAuth(s.MuteHandler()), mdlw))
	s.router.POST("/mute", httproutermiddleware.Handler("mute", s.am.MustAuth(s.MuteHandler()), mdlw))
	s.router.GET("/muted", httproutermiddleware.Handler("muted", s.am.MustAuth(s.MutedHandler()), mdlw))
	s.router.GET("/unmute", httproutermiddleware.Handler("unmute", s.am.MustAuth(s.UnmuteHandler()), mdlw))
	s.router.POST("/unmute", httproutermiddleware.Handler("unmute", s.am.MustAuth(s.UnmuteHandler()), mdlw))

	s.router.GET("/mute/:hash", httproutermiddleware.Handler("mute", s.am.MustAuth(s.MuteHandler()), mdlw))
	s.router.POST("/mute/:hash", httproutermiddleware.Handler("mute", s.am.MustAuth(s.MuteHandler()), mdlw))
	s.router.GET("/unmute/:hash", httproutermiddleware.Handler("unmute", s.am.MustAuth(s.UnmuteHandler()), mdlw))
	s.router.POST("/unmute/:hash", httproutermiddleware.Handler("unmute", s.am.MustAuth(s.UnmuteHandler()), mdlw))

	s.router.GET("/settings", httproutermiddleware.Handler("settings", s.am.MustAuth(s.SettingsHandler()), mdlw))
	s.router.POST("/settings", httproutermiddleware.Handler("settings", s.am.MustAuth(s.SettingsHandler()), mdlw))
	s.router.POST("/settings/addlink", httproutermiddleware.Handler("settings_addlink", s.am.MustAuth(s.SettingsAddLinkHandler()), mdlw))
	s.router.POST("/settings/removelink", httproutermiddleware.Handler("settings_removelink", s.am.MustAuth(s.SettingsRemoveLinkHandler()), mdlw))

	s.router.GET("/info", httproutermiddleware.Handler("info", s.PodInfoHandler(), mdlw))
	s.router.GET("/config", httproutermiddleware.Handler("config", s.am.MustAuth(s.PodConfigHandler()), mdlw))
	s.router.GET("/manage/pod", httproutermiddleware.Handler("manage_pod", s.am.MustAuth(s.ManagePodHandler()), mdlw))
	s.router.GET("/manage/jobs", httproutermiddleware.Handler("manage_jobs", s.am.MustAuth(s.ManageJobsHandler()), mdlw))
	s.router.POST("/manage/jobs", httproutermiddleware.Handler("manage_jobs", s.am.MustAuth(s.ManageJobsHandler()), mdlw))
	s.router.GET("/manage/peers", httproutermiddleware.Handler("manage_peers", s.am.MustAuth(s.ManagePeersHandler()), mdlw))
	s.router.POST("/manage/pod", httproutermiddleware.Handler("manage_pod", s.am.MustAuth(s.ManagePodHandler()), mdlw))
	s.router.GET("/manage/refreshcache", httproutermiddleware.Handler("manage_refreshcache", s.am.MustAuth(s.RefreshCacheHandler()), mdlw))

	s.router.GET("/manage/users", httproutermiddleware.Handler("manager_users", s.am.MustAuth(s.ManageUsersHandler()), mdlw))
	s.router.POST("/manage/adduser", httproutermiddleware.Handler("adduser", s.am.MustAuth(s.AddUserHandler()), mdlw))
	s.router.POST("/manage/delfeed", httproutermiddleware.Handler("delfeed", s.am.MustAuth(s.DelFeedHandler()), mdlw))
	s.router.POST("/manage/deluser", httproutermiddleware.Handler("deluser", s.am.MustAuth(s.DelUserHandler()), mdlw))
	s.router.POST("/manage/rstuser", httproutermiddleware.Handler("rstuser", s.am.MustAuth(s.RstUserHandler()), mdlw))

	s.router.POST("/delete", httproutermiddleware.Handler("delete", s.am.MustAuth(s.DeleteHandler()), mdlw))

	// Support / Report Abuse handlers
	s.router.GET("/support", httproutermiddleware.Handler("support", s.SupportHandler(), mdlw))
	s.router.POST("/support", httproutermiddleware.Handler("support", s.SupportHandler(), mdlw))
	s.router.GET("/_captcha", httproutermiddleware.Handler("captcha", s.CaptchaHandler(), mdlw))

	s.router.GET("/report", httproutermiddleware.Handler("report", s.ReportHandler(), mdlw))
	s.router.POST("/report", httproutermiddleware.Handler("report", s.ReportHandler(), mdlw))
}

// NewServer ...
func NewServer(bind string, options ...Option) (*Server, error) {
	config := NewConfig()

	for _, opt := range options {
		if err := opt(config); err != nil {
			return nil, err
		}
	}

	settingsFn := filepath.Join(config.Data, "settings.yaml")
	if FileExists(settingsFn) {
		if settings, err := LoadSettings(settingsFn); err != nil {
			log.Warnf("error loading pod settings from %s: %s", settingsFn, err)
		} else {
			if err := merger.MergeOverwrite(config, settings); err != nil {
				log.WithError(err).Error("error merging pod settings")
				return nil, err
			}
		}
	}

	if err := config.Validate(); err != nil {
		log.WithError(err).Error("error validating config")
		return nil, fmt.Errorf("error validating config: %w", err)
	}

	log.Debugf("Before Cache: %s", MemoryUsage())
	cache, err := LoadCache(config)
	if err != nil {
		log.WithError(err).Error("error loading feed cache")
		return nil, err
	}
	log.Debugf("After Cache: %s", MemoryUsage())

	archive, err := NewDiskArchiver(filepath.Join(config.Data, archiveDir))
	if err != nil {
		log.WithError(err).Error("error creating feed archiver")
		return nil, err
	}

	db, err := NewStore(config.Store)
	if err != nil {
		log.WithError(err).Error("error creating store")
		return nil, err
	}

	// translator
	translator, err := NewTranslator()
	if err != nil {
		log.WithError(err).Error("error loading translator")
		return nil, err
	}

	tmplman, err := NewTemplateManager(config, translator, cache, archive)
	if err != nil {
		log.WithError(err).Error("error creating template manager")
		return nil, err
	}

	router := NewRouter()

	am := auth.NewManager(auth.NewOptions("/login", "/register"))

	tasks := NewDispatcher(10, 100) // TODO: Make this configurable?

	pm := passwords.NewScryptPasswords(nil)

	sc := NewSessionStore(db, config.SessionCacheTTL)

	sm := session.NewManager(
		session.NewOptions(
			"yarnd_token",
			config.CookieSecret,
			config.LocalURL().Scheme == "https",
			config.SessionExpiry,
		),
		sc,
	)

	api := NewAPI(router, config, cache, archive, db, pm, tasks)

	var handler http.Handler

	csrfHandler := nosurf.New(router)
	csrfHandler.ExemptGlob("/api/v1/*")
	csrfHandler.ExemptGlob("/indieauth/*")
	csrfHandler.ExemptPath("/webmention")
	csrfHandler.ExemptPath("/websub")
	csrfHandler.ExemptPath("/notify")

	// Useful for Safari / Mobile Safari when behind Cloudflare to streaming
	// videos _actually_ works :O
	if config.DisableGzip {
		handler = sm.Handler(csrfHandler)
	} else {
		handler = gziphandler.GzipHandler(sm.Handler(csrfHandler))
	}

	if !config.DisableLogger {
		handler = logger.New(logger.Options{
			Prefix:               "yarnd",
			RemoteAddressHeaders: []string{"X-Forwarded-For"},
		}).Handler(handler)
	}

	server := &Server{
		bind:    bind,
		config:  config,
		router:  router,
		tmplman: tmplman,

		server: &http.Server{Addr: bind, Handler: handler},

		// API
		api: api,

		// Feed Cache
		cache: cache,

		// Feed Archiver
		archive: archive,

		// Data Store
		db: db,

		// Schedular
		cron: cron.New(),

		// Dispatcher
		tasks: tasks,

		// Auth Manager
		am: am,

		// Session Manager
		sc: sc,
		sm: sm,

		// Password Manager
		pm: pm,

		// Translator
		translator: translator,
	}

	// Factory functions that require access to the Pod Config and Store
	server.AppendTwt = AppendTwtFactory(config, cache, db)
	server.FilterTwts = FilterTwtsFactory(config)

	if err := server.setupJobs(); err != nil {
		log.WithError(err).Error("error setting up background jobs")
		return nil, err
	}
	server.cron.Start()
	log.Info("started background jobs")

	server.tasks.Start()
	log.Info("started task dispatcher")

	server.setupWebMentions()
	log.Infof("started webmentions processor")

	server.setupWebSub()
	log.Infof("started websub processor")

	server.setupMetrics()
	log.Infof("serving metrics endpoint at %s/metrics", server.config.BaseURL)

	// Log interesting configuration options
	log.Infof("Debug: %t", server.config.Debug)
	log.Infof("Instance Name: %s", server.config.Name)
	log.Infof("Base URL: %s", server.config.BaseURL)
	log.Infof("Using Theme: %s", server.config.Theme)
	log.Infof("Admin User: %s", server.config.AdminUser)
	log.Infof("Admin Name: %s", server.config.AdminName)
	log.Infof("Admin Email: %s", server.config.AdminEmail)
	log.Infof("Max Twts per Page: %d", server.config.TwtsPerPage)
	log.Infof("Max Cache TTL: %s", server.config.MaxCacheTTL)
	log.Infof("Fetch Interval: %s", server.config.FetchInterval)
	log.Infof("Max Cache Items: %d", server.config.MaxCacheItems)
	log.Infof("Maximum length of Posts: %d", server.config.MaxTwtLength)
	log.Infof("Open User Profiles: %t", server.config.OpenProfiles)
	log.Infof("Open Registrations: %t", server.config.OpenRegistrations)
	log.Infof("Disable Gzip: %t", server.config.DisableGzip)
	log.Infof("Disable Logger: %t", server.config.DisableLogger)
	log.Infof("Disable Media: %t", server.config.DisableMedia)
	log.Infof("Disable FFMpeg: %t", server.config.DisableFfmpeg)
	log.Infof("SMTP Host: %s", server.config.SMTPHost)
	log.Infof("SMTP Port: %d", server.config.SMTPPort)
	log.Infof("SMTP User: %s", server.config.SMTPUser)
	log.Infof("SMTP From: %s", server.config.SMTPFrom)
	log.Infof("Max Fetch Limit: %s", humanize.Bytes(uint64(server.config.MaxFetchLimit)))
	log.Infof("Max Upload Size: %s", humanize.Bytes(uint64(server.config.MaxUploadSize)))
	log.Infof("API Session Time: %s", server.config.APISessionTime)
	log.Infof("Enabled Features: %s", server.config.Features)

	// Warn about user registration being disabled.
	if !server.config.OpenRegistrations {
		log.Warn("registrations are disabled as per configuration (no -R/--open-registrations)")
	}

	// Warn about `ffmpeg` not installed or available
	if !CmdExists("ffmpeg") {
		log.Warn("ffmpeg not found, audio and video support will be disabled")
		server.config.DisableFfmpeg = true
	}

	server.initRoutes()
	api.initRoutes()

	go server.runStartupJobs()

	return server, nil
}

func (s *Server) tr(ctx *Context, msgID string, data ...interface{}) string {
	return s.translator.Translate(ctx, msgID, data...)
}
