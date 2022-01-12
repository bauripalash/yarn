package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/rickb777/accept"
	"github.com/securisec/go-keywords"
	log "github.com/sirupsen/logrus"
	"github.com/vcraescu/go-paginator"
	"github.com/vcraescu/go-paginator/adapter"

	"git.mills.io/yarnsocial/yarn/types"
)

// ConversationHandler ...
func (s *Server) ConversationHandler() httprouter.Handle {
	isLocal := IsLocalURLFactory(s.config)

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		ctx := NewContext(s, r)
		ctx.Translate(s.translator)

		hash := p.ByName("hash")
		if hash == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		var err error

		twt, inCache := s.cache.Lookup(hash)
		if !inCache {
			// If the twt is not in the cache look for it in the archive
			if s.archive.Has(hash) {
				twt, err = s.archive.Get(hash)
				if err != nil {
					ctx.Error = true
					ctx.Message = "Error loading twt from archive, please try again"
					s.render("error", w, ctx)
					return
				}
			}
		}

		if twt.IsZero() {
			ctx.Error = true
			ctx.Message = "No matching twt found!"
			s.render("404", w, ctx)
			return
		}

		var (
			who   string
			image string
		)

		twter := twt.Twter()
		if isLocal(twter.URI) {
			who = fmt.Sprintf("%s@%s", twter.Nick, s.config.LocalURL().Hostname())
			image = URLForAvatar(s.config.BaseURL, twter.Nick, "")
		} else {
			who = fmt.Sprintf("@<%s %s>", twter.Nick, twter.URI)
			image = URLForExternalAvatar(s.config, twter.URI)
		}

		when := twt.Created().Format(time.RFC3339)
		what := twt.FormatText(types.TextFmt, s.config)

		var ks []string
		if ks, err = keywords.Extract(what); err != nil {
			log.WithError(err).Warn("error extracting keywords")
		}

		for _, m := range twt.Mentions() {
			ks = append(ks, m.Twter().Nick)
		}
		var tags types.TagList = twt.Tags()
		ks = append(ks, tags.Tags()...)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.HasPrefix(twt.Twter().URI, s.config.BaseURL) {
			w.Header().Set(
				"Link",
				fmt.Sprintf(
					`<%s/user/%s/webmention>; rel="webmention"`,
					s.config.BaseURL, twt.Twter().Nick,
				),
			)
		}

		twts := s.cache.GetByUserView(ctx.User, fmt.Sprintf("subject:(#%s)", hash), false)[:]
		if !inCache {
			twts = append(twts, twt)
		}
		sort.Sort(sort.Reverse(twts))

		if len(twts) == 0 {
			ctx.Error = true
			ctx.Message = "No matching twts found due to muted feeds"
			s.render("404", w, ctx)
			return
		}

		if accept.PreferredContentTypeLike(r.Header, "application/json") == "application/json" {
			data, err := json.Marshal(twts)
			if err != nil {
				log.WithError(err).Error("error serializing twt response")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Last-Modified", twt.Created().Format(http.TimeFormat))
			_, _ = w.Write(data)
			return
		}

		var pagedTwts types.Twts

		page := SafeParseInt(r.FormValue("p"), 1)
		pager := paginator.New(adapter.NewSliceAdapter(twts), s.config.TwtsPerPage)
		pager.SetPage(page)

		if err := pager.Results(&pagedTwts); err != nil {
			ctx.Error = true
			ctx.Message = "An error occurred while loading search results"
			s.render("error", w, ctx)
			return
		}

		if r.Method == http.MethodHead {
			defer r.Body.Close()
			return
		}

		title := fmt.Sprintf("%s \"%s\"", who, what)

		ctx.Title = title
		ctx.Meta = Meta{
			Title:       fmt.Sprintf("%s #%s", s.tr(ctx, "ConversationTitle"), twt.Hash()),
			Description: what,
			UpdatedAt:   when,
			Author:      who,
			Image:       image,
			URL:         URLForTwt(s.config.BaseURL, hash),
			Keywords:    strings.Join(ks, ", "),
		}

		if strings.HasPrefix(twt.Twter().URI, s.config.BaseURL) {
			ctx.Links = append(ctx.Links, Link{
				Href: fmt.Sprintf("%s/webmention", UserURL(twt.Twter().URI)),
				Rel:  "webmention",
			})
			ctx.Alternatives = append(ctx.Alternatives, Alternatives{
				Alternative{
					Type:  "text/plain",
					Title: fmt.Sprintf("%s's Twtxt Feed", twt.Twter().Nick),
					URL:   twt.Twter().URI,
				},
				Alternative{
					Type:  "application/atom+xml",
					Title: fmt.Sprintf("%s's Atom Feed", twt.Twter().Nick),
					URL:   fmt.Sprintf("%s/atom.xml", UserURL(twt.Twter().URI)),
				},
			}...)
		}

		if ctx.Authenticated {
			lastTwt, _, err := GetLastTwt(s.config, ctx.User)
			if err != nil {
				log.WithError(err).Error("error getting user last twt")
				ctx.Error = true
				ctx.Message = "An error occurred while loading the timeline"
				s.render("error", w, ctx)
				return
			}
			ctx.LastTwt = lastTwt
		}

		ctx.Reply = fmt.Sprintf("#%s", twt.Hash())
		ctx.Twts = pagedTwts
		ctx.Pager = &pager
		s.render("conversation", w, ctx)
	}
}
