// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
	"github.com/vcraescu/go-paginator"
	"github.com/vcraescu/go-paginator/adapter"

	"git.mills.io/yarnsocial/yarn/internal/passwords"
	"go.yarn.social/types"
)

// ContextKey ...
type ContextKey int

const (
	TokenContextKey ContextKey = iota
	UserContextKey
)

var (
	// ErrInvalidCredentials is returned for invalid credentials against /auth
	ErrInvalidCredentials = errors.New("error: invalid credentials")

	// ErrInvalidToken is returned for expired or invalid tokens used in Authorizeation headers
	ErrInvalidToken = errors.New("error: invalid token")
)

// Token ...
type Token struct {
	Signature string
	Value     string
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// API ...
type API struct {
	router  *Router
	config  *Config
	cache   *Cache
	archive Archiver
	db      Store
	pm      passwords.Passwords
	tasks   *Dispatcher
}

// NewAPI ...
func NewAPI(router *Router, config *Config, cache *Cache, archive Archiver, db Store, pm passwords.Passwords, tasks *Dispatcher) *API {
	api := &API{router, config, cache, archive, db, pm, tasks}

	api.initRoutes()

	return api
}

func (a *API) initRoutes() {
	router := a.router.Group("/api/v1")

	router.GET("/ping", a.PingEndpoint())
	router.POST("/auth", a.AuthEndpoint())
	router.POST("/register", a.RegisterEndpoint())
	router.GET("/config", a.PodConfigEndpoint())

	router.POST("/post", a.isAuthorized(a.PostEndpoint()))
	router.POST("/upload", a.isAuthorized(a.UploadMediaEndpoint()))

	router.GET("/settings", a.isAuthorized(a.SettingsEndpoint()))
	router.POST("/settings", a.isAuthorized(a.SettingsEndpoint()))

	router.POST("/follow", a.isAuthorized(a.FollowEndpoint()))
	router.POST("/unfollow", a.isAuthorized(a.UnfollowEndpoint()))

	router.POST("/mute", a.isAuthorized(a.MuteEndpoint()))
	router.POST("/unmute", a.isAuthorized(a.UnmuteEndpoint()))

	router.POST("/timeline", a.isAuthorized(a.TimelineEndpoint()))
	router.POST("/discover", a.DiscoverEndpoint())

	router.GET("/profile", a.ProfileEndpoint())
	router.GET("/profile/:username", a.ProfileEndpoint())
	router.POST("/fetch-twts", a.FetchTwtsEndpoint())
	router.POST("/conv", a.ConversationEndpoint())

	router.POST("/external", a.ExternalProfileEndpoint())

	router.POST("/mentions", a.isAuthorized(a.MentionsEndpoint()))

	// WebSub (debugging)
	router.GET("/websub", a.isAuthorized(a.WebSubEndpoint()))

	// Support / Report endpoints
	router.POST("/support", a.isAuthorized(a.SupportEndpoint()))
	router.POST("/report", a.isAuthorized(a.ReportEndpoint()))
}

// CreateToken ...
func (a *API) CreateToken(user *User, r *http.Request) (*Token, error) {
	claims := jwt.MapClaims{}
	claims["username"] = user.Username
	createdAt := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(a.config.APISigningKey))
	if err != nil {
		log.WithError(err).Error("error creating signed token")
		return nil, err
	}

	signedToken, err := jwt.Parse(tokenString, a.jwtKeyFunc)
	if err != nil {
		log.WithError(err).Error("error creating signed token")
		return nil, err
	}

	tkn := &Token{
		Signature: signedToken.Signature,
		Value:     tokenString,
		UserAgent: r.UserAgent(),
		CreatedAt: createdAt,
	}

	return tkn, nil
}

func (a *API) jwtKeyFunc(token *jwt.Token) (interface{}, error) {
	if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("there was an error")
	}
	return []byte(a.config.APISigningKey), nil
}

func (a *API) getLoggedInUser(r *http.Request) *User {
	token, err := jwt.Parse(r.Header.Get("Token"), a.jwtKeyFunc)
	if err != nil {
		return nil
	}

	if !token.Valid {
		return nil
	}

	claims := token.Claims.(jwt.MapClaims)

	username := claims["username"].(string)

	user, err := a.db.GetUser(username)
	if err != nil {
		log.WithError(err).Error("error loading user object")
		return nil
	}

	// Every registered new user follows themselves
	// TODO: Make  this configurable server behaviour?
	if user.Following == nil {
		user.Following = make(map[string]string)
	}

	user.Follow(user.Username, user.URL)

	return user
}

func (a *API) isAuthorized(endpoint httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		if r.Header.Get("Token") == "" {
			http.Error(w, "No Token Provided", http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(r.Header.Get("Token"), a.jwtKeyFunc)
		if err != nil {
			log.WithError(err).Error("error parsing token")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		if token.Valid {
			claims := token.Claims.(jwt.MapClaims)

			username := claims["username"].(string)

			user, err := a.db.GetUser(username)
			if err != nil {
				log.WithError(err).Error("error loading user object")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Every registered new user follows themselves
			// TODO: Make  this configurable server behaviour?
			if user.Following == nil {
				user.Following = make(map[string]string)
			}
			user.Follow(user.Username, user.URL)

			ctx := context.WithValue(r.Context(), TokenContextKey, token)
			ctx = context.WithValue(ctx, UserContextKey, user)

			// TODO: Use event sourcing for this?
			user.LastSeenAt = time.Now().Round(24 * time.Hour)
			if err := a.db.SetUser(user.Username, user); err != nil {
				log.WithError(err).Warnf("error updating user.LastSeenAt for %s", user.Username)
			}

			endpoint(w, r.WithContext(ctx), p)
		} else {
			http.Error(w, "Invalid Token", http.StatusUnauthorized)
			return
		}
	}
}

// PingEndpoint ...
func (a *API) PingEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

// RegisterEndpoint ...
func (a *API) RegisterEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		req, err := types.NewRegisterRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing register request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		username := NormalizeUsername(req.Username)
		password := req.Password
		// XXX: We DO NOT store this! (EVER)
		email := strings.TrimSpace(req.Email)

		if err := ValidateUsername(username); err != nil {
			http.Error(w, "Bad Username", http.StatusBadRequest)
			return
		}

		if a.db.HasUser(username) || a.db.HasFeed(username) {
			http.Error(w, "Username Exists", http.StatusBadRequest)
			return
		}

		fn := filepath.Join(a.config.Data, feedsDir, username)
		if _, err := os.Stat(fn); err == nil {
			http.Error(w, "Feed Exists", http.StatusBadRequest)
			return
		}

		if err := ioutil.WriteFile(fn, []byte{}, 0644); err != nil {
			log.WithError(err).Error("error creating new user feed")
			http.Error(w, "Feed Creation Failed", http.StatusInternalServerError)
			return
		}

		hash, err := a.pm.CreatePassword(password)
		if err != nil {
			log.WithError(err).Error("error creating password hash")
			http.Error(w, "Passwrod Creation Failed", http.StatusInternalServerError)
			return
		}

		recoveryHash := fmt.Sprintf("email:%s", FastHashString(email))

		user := &User{
			Username:  username,
			Password:  hash,
			Recovery:  recoveryHash,
			URL:       URLForUser(a.config.BaseURL, username),
			CreatedAt: time.Now(),
		}

		if err := a.db.SetUser(username, user); err != nil {
			log.WithError(err).Error("error saving user object for new user")
			http.Error(w, "User Creation Failed", http.StatusInternalServerError)
			return
		}
	}
}

// AuthEndpoint ...
func (a *API) AuthEndpoint() httprouter.Handle {
	// #239: Throttle failed login attempts and lock user  account.
	failures := NewTTLCache(5 * time.Minute)

	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		req, err := types.NewAuthRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing auth request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		username := NormalizeUsername(req.Username)
		password := req.Password

		// Error: no username or password provided
		if username == "" || password == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Lookup user
		user, err := a.db.GetUser(username)
		if err != nil {
			log.WithField("username", username).Warn("login attempt from non-existent user")
			http.Error(w, "Invalid Credentials", http.StatusUnauthorized)
			return
		}

		// #239: Throttle failed login attempts and lock user  account.
		if failures.Get(user.Username) > MaxFailedLogins {
			http.Error(w, "Account Locked", http.StatusTooManyRequests)
			return
		}

		// Validate cleartext password against KDF hash
		err = a.pm.CheckPassword(user.Password, password)
		if err != nil {
			// #239: Throttle failed login attempts and lock user  account.
			failed := failures.Inc(user.Username)
			time.Sleep(time.Duration(IntPow(2, failed)) * time.Second)

			log.WithField("username", username).Warn("login attempt with invalid credentials")
			http.Error(w, "Invalid Credentials", http.StatusUnauthorized)
			return
		}

		// #239: Throttle failed login attempts and lock user  account.
		failures.Reset(user.Username)

		// Login successful
		log.WithField("username", username).Info("login successful")

		token, err := a.CreateToken(user, r)
		if err != nil {
			log.WithError(err).Error("error creating token")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		res := types.AuthResponse{Token: token.Value}

		body, err := res.Bytes()
		if err != nil {
			log.WithError(err).Error("error serializing response")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// PostEndpoint ...
func (a *API) PostEndpoint() httprouter.Handle {
	appendTwt := AppendTwtFactory(a.config, a.cache, a.db)

	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		user := r.Context().Value(UserContextKey).(*User)

		req, err := types.NewPostRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing post request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		text := CleanTwt(req.Text)
		if text == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		var sources types.FetchFeedRequests

		switch req.PostAs {
		case "", me:
			sources = user.Source()
			_, err = appendTwt(user, nil, text)
		default:
			if user.OwnsFeed(req.PostAs) {
				feed, feedErr := a.db.GetFeed(req.PostAs)
				if feedErr != nil {
					log.WithError(err).Error("error posting twt")
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				sources = feed.Source()

				_, err = appendTwt(user, feed, text)
			} else {
				err = ErrFeedImposter
			}
		}

		if err != nil {
			log.WithError(err).Error("error posting twt")
			if err == ErrFeedImposter {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			} else {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}

		// Update user's own timeline with their own new post.
		a.cache.FetchFeeds(a.config, a.archive, sources, nil)

		// Re-populate/Warm cache for User
		a.cache.GetByUser(user, true)

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

// TimelineEndpoint ...
func (a *API) TimelineEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		user := r.Context().Value(UserContextKey).(*User)

		req, err := types.NewPagedRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing post request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		twts := a.cache.GetByUser(user, false)

		var pagedTwts types.Twts

		pager := paginator.New(adapter.NewSliceAdapter(twts), a.config.TwtsPerPage)
		pager.SetPage(req.Page)

		if err = pager.Results(&pagedTwts); err != nil {
			log.WithError(err).Error("error loading timeline")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		res := types.PagedResponse{
			Twts: pagedTwts,
			Pager: types.PagerResponse{
				Current:   pager.Page(),
				MaxPages:  pager.PageNums(),
				TotalTwts: pager.Nums(),
			},
		}

		body, err := res.Bytes()
		if err != nil {
			log.WithError(err).Error("error serializing response")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// DiscoverEndpoint ...
func (a *API) DiscoverEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		loggedInUser := a.getLoggedInUser(r)

		req, err := types.NewPagedRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing post request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		twts := a.cache.GetByUserView(loggedInUser, discoverViewKey, false)

		var pagedTwts types.Twts

		pager := paginator.New(adapter.NewSliceAdapter(twts), a.config.TwtsPerPage)
		pager.SetPage(req.Page)

		if err = pager.Results(&pagedTwts); err != nil {
			log.WithError(err).Error("error loading discover")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		res := types.PagedResponse{
			Twts: pagedTwts,
			Pager: types.PagerResponse{
				Current:   pager.Page(),
				MaxPages:  pager.PageNums(),
				TotalTwts: pager.Nums(),
			},
		}

		body, err := res.Bytes()
		if err != nil {
			log.WithError(err).Error("error serializing response")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// MentionsEndpoint ...
func (a *API) MentionsEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		req, err := types.NewPagedRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing post request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		user := r.Context().Value(UserContextKey).(*User)

		twts := a.cache.GetMentions(user, false)
		sort.Sort(twts)

		var pagedTwts types.Twts

		pager := paginator.New(adapter.NewSliceAdapter(twts), a.config.TwtsPerPage)
		pager.SetPage(req.Page)

		if err = pager.Results(&pagedTwts); err != nil {
			log.WithError(err).Error("error loading discover")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		res := types.PagedResponse{
			Twts: pagedTwts,
			Pager: types.PagerResponse{
				Current:   pager.Page(),
				MaxPages:  pager.PageNums(),
				TotalTwts: pager.Nums(),
			},
		}

		body, err := res.Bytes()
		if err != nil {
			log.WithError(err).Error("error serializing response")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// FollowEndpoint ...
func (a *API) FollowEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		user := r.Context().Value(UserContextKey).(*User)

		req, err := types.NewFollowRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing follow request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		nick := strings.TrimSpace(req.Nick)
		url := NormalizeURL(req.URL)

		if nick == "" || url == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		if err := user.FollowAndValidate(a.config, nick, url); err != nil {
			log.WithError(err).Errorf("error validating new feed @<%s %s>", nick, url)
			http.Error(w, "Invalid Feed", http.StatusBadRequest)
			return
		}

		if err := a.db.SetUser(user.Username, user); err != nil {
			log.WithError(err).Error("error saving user object")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		a.cache.GetByUser(user, true)

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

// UnfollowEndpoint ...
func (a *API) UnfollowEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		req, err := types.NewUnfollowRequest(r.Body)

		if err != nil {
			log.WithError(err).Error("error parsing follow request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		nick := req.Nick

		if nick == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		user := r.Context().Value(UserContextKey).(*User)

		if user == nil {
			log.Fatalf("user not found in context")
		}

		if _, ok := user.Following[nick]; !ok {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		user.Unfollow(nick)

		if err := a.db.SetUser(user.Username, user); err != nil {
			log.WithError(err).Warnf("error updating user object for user  %s", user.Username)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		a.cache.GetByUser(user, true)

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

// SettingsEndpoint ...
func (a *API) SettingsEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		// Limit request body to to abuse
		r.Body = http.MaxBytesReader(w, r.Body, a.config.MaxUploadSize)
		defer r.Body.Close()

		user := r.Context().Value(UserContextKey).(*User)

		if r.Method == http.MethodGet {
			data, err := json.Marshal(user)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}

		// XXX: We DO NOT store this! (EVER)
		email := strings.TrimSpace(r.FormValue("email"))
		tagline := strings.TrimSpace(r.FormValue("tagline"))
		password := r.FormValue("password")

		isFollowersPubliclyVisible := r.FormValue("isFollowersPubliclyVisible") == "on"
		isFollowingPubliclyVisible := r.FormValue("isFollowingPubliclyVisible") == "on"

		avatarFile, _, err := r.FormFile("avatar_file")
		if err != nil && err != http.ErrMissingFile {
			log.WithError(err).Error("error parsing form file")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if password != "" {
			hash, err := a.pm.CreatePassword(password)
			if err != nil {
				log.WithError(err).Error("error creating password hash")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			user.Password = hash
		}

		if avatarFile != nil {
			opts := &ImageOptions{
				Resize: true,
				Width:  a.config.AvatarResolution,
				Height: a.config.AvatarResolution,
			}
			_, err = StoreUploadedImage(
				a.config, avatarFile,
				avatarsDir, user.Username,
				opts,
			)
			if err != nil {
				log.WithError(err).Error("error updating user avatar")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			avatarFn := filepath.Join(a.config.Data, avatarsDir, fmt.Sprintf("%s.png", user.Username))
			if avatarHash, err := FastHashFile(avatarFn); err == nil {
				user.AvatarHash = avatarHash
			} else {
				log.WithError(err).Warnf("error updating avatar hash for %s", user.Username)
			}
		}

		recoveryHash := fmt.Sprintf("email:%s", FastHashString(email))

		user.Recovery = recoveryHash
		user.Tagline = tagline

		// XXX: Commented out as these are more specific to the Web App currently.
		// API clients such as Goryon (the Flutter iOS/Android app) have their own mechanisms.
		// user.Theme = theme
		// user.DisplayDatesInTimezone = displayDatesInTimezone
		// user.DisplayImagesPreferences = displayImagesPreference

		user.IsFollowersPubliclyVisible = isFollowersPubliclyVisible
		user.IsFollowingPubliclyVisible = isFollowingPubliclyVisible

		if err := a.db.SetUser(user.Username, user); err != nil {
			log.WithError(err).Error("error updating user object")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

// OldUploadMediaEndpoint ...
// TODO: Remove when the api_old_upload_media counter nears zero
// XXX: Used for Goryon < v1.0.3
func (a *API) OldUploadMediaEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		// Limit request body to to abuse
		r.Body = http.MaxBytesReader(w, r.Body, a.config.MaxUploadSize)
		defer r.Body.Close()

		mediaFile, _, err := r.FormFile("media_file")
		if err != nil && err != http.ErrMissingFile {
			log.WithError(err).Error("error parsing form file")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var mediaURI string

		if mediaFile != nil {
			opts := &ImageOptions{Resize: true, Width: a.config.MediaResolution, Height: 0}
			mediaURI, err = StoreUploadedImage(
				a.config, mediaFile,
				mediaDir, "",
				opts,
			)

			if err != nil {
				log.WithError(err).Error("error storing the file")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		uri := URI{"mediaURI", mediaURI}
		data, err := json.Marshal(uri)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// UploadMediaHandler ...
func (a *API) UploadMediaEndpoint() httprouter.Handle {
	oldUploadMediaEndpoint := a.OldUploadMediaEndpoint()
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		// Support for older clients pre v1.0.3 (See: OldMediaEndpoint)
		//
		if strings.HasPrefix(r.UserAgent(), "Dart") {
			oldUploadMediaEndpoint(w, r, p)
			return
		}

		// Limit request body to to abuse
		r.Body = http.MaxBytesReader(w, r.Body, a.config.MaxUploadSize)

		mfile, headers, err := r.FormFile("media_file")
		if err != nil && err != http.ErrMissingFile {
			if err.Error() == "http: request body too large" {
				http.Error(w, "Media Upload Too Large", http.StatusRequestEntityTooLarge)
				return
			}
			log.WithError(err).Error("error parsing form file")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if mfile == nil || headers == nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		ctype := headers.Header.Get("Content-Type")

		var uri URI

		if strings.HasPrefix(ctype, "image/") {
			fn, err := ReceiveImage(mfile)
			if err != nil {
				log.WithError(err).Error("error writing uploaded image")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			uuid, err := a.tasks.Dispatch(NewImageTask(a.config, fn))
			if err != nil {
				log.WithError(err).Error("error dispatching image processing task")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			uri.Type = "taskURI"
			uri.Path = URLForTask(a.config.BaseURL, uuid)
		}

		if strings.HasPrefix(ctype, "audio/") {
			fn, err := ReceiveAudio(mfile)
			if err != nil {
				log.WithError(err).Error("error writing uploaded audio")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			uuid, err := a.tasks.Dispatch(NewAudioTask(a.config, fn))
			if err != nil {
				log.WithError(err).Error("error dispatching audio transcoding task")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			uri.Type = "taskURI"
			uri.Path = URLForTask(a.config.BaseURL, uuid)
		}

		if strings.HasPrefix(ctype, "video/") {
			fn, err := ReceiveVideo(mfile)
			if err != nil {
				log.WithError(err).Error("error writing uploaded video")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			uuid, err := a.tasks.Dispatch(NewVideoTask(a.config, fn))
			if err != nil {
				log.WithError(err).Error("error dispatching vodeo transcode task")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			uri.Type = "taskURI"
			uri.Path = URLForTask(a.config.BaseURL, uuid)
		}

		if uri.IsZero() {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		data, err := json.Marshal(uri)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if uri.Type == "taskURI" {
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write(data)
	}
}

// ProfileEndpoint ...
func (a *API) ProfileEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		loggedInUser := a.getLoggedInUser(r)

		username := NormalizeUsername(p.ByName("username"))
		if username == "" {
			username = loggedInUser.Username
		}

		var profile types.Profile

		if a.db.HasUser(username) {
			user, err := a.db.GetUser(username)
			if err != nil {
				log.WithError(err).Errorf("error loading user object for %s", username)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			profile = user.Profile(a.config.BaseURL, loggedInUser)
		} else if a.db.HasFeed(username) {
			feed, err := a.db.GetFeed(username)
			if err != nil {
				log.WithError(err).Errorf("error loading feed object for %s", username)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			profile = feed.Profile(a.config.BaseURL, loggedInUser)
		} else {
			http.Error(w, "User/Feed not found", http.StatusNotFound)
			return
		}

		if !a.cache.IsCached(profile.URI) {
			sources := make(types.FetchFeedRequests)
			sources[types.FetchFeedRequest{Nick: profile.Nick, URL: profile.URI}] = true
			a.cache.FetchFeeds(a.config, a.archive, sources, nil)
		}

		var twter types.Twter

		if cachedTwter := a.cache.GetTwter(profile.URI); cachedTwter != nil {
			twter = *cachedTwter
		} else {
			twter = types.Twter{Nick: profile.Nick, URI: profile.URI}
		}

		followers := a.cache.GetFollowers(profile)
		profile.Followers = followers
		profile.NFollowers = len(followers)

		profileResponse := types.ProfileResponse{
			Profile: profile.AsOldProfile(),
			Twter:   twter,
		}

		data, err := json.Marshal(profileResponse)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// ConversationEndpoint ...
func (a *API) ConversationEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		loggedInUser := a.getLoggedInUser(r)

		req, err := types.NewConversationRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing conversation request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		hash := req.Hash

		if hash == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		twt, inCache := a.cache.Lookup(hash)
		if !inCache {
			// If the twt is not in the cache look for it in the archive
			if a.archive.Has(hash) {
				twt, err = a.archive.Get(hash)
				if err != nil {
					log.WithError(err).Errorf("error fetching twt %s from archive", hash)
					http.Error(w, "Bad Request", http.StatusBadRequest)
					return
				}
			}
		}

		if twt.IsZero() {
			http.Error(w, "Conversation Not Found", http.StatusNotFound)
			return
		}

		twts := a.cache.GetByUserView(loggedInUser, fmt.Sprintf("subject:(#%s)", hash), false)[:]
		if !inCache {
			twts = append(twts, twt)
		}
		sort.Sort(sort.Reverse(twts))

		var pagedTwts types.Twts

		pager := paginator.New(adapter.NewSliceAdapter(twts), a.config.TwtsPerPage)
		pager.SetPage(req.Page)

		if err = pager.Results(&pagedTwts); err != nil {
			log.WithError(err).Error("error loading twts")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		res := types.PagedResponse{
			Twts: pagedTwts,
			Pager: types.PagerResponse{
				Current:   pager.Page(),
				MaxPages:  pager.PageNums(),
				TotalTwts: pager.Nums(),
			},
		}

		data, err := json.Marshal(res)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// FetchTwtsEndpoint ...
func (a *API) FetchTwtsEndpoint() httprouter.Handle {
	isLocal := IsLocalURLFactory(a.config)

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		loggedInUser := a.getLoggedInUser(r)

		req, err := types.NewFetchTwtsRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing fetch twts request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		nick := NormalizeUsername(req.Nick)
		if nick == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		var profile types.Profile
		var twts types.Twts

		if req.URL != "" && !isLocal(req.URL) {
			if !a.cache.IsCached(req.URL) {
				sources := make(types.FetchFeedRequests)
				sources[types.FetchFeedRequest{Nick: nick, URL: req.URL}] = true
				a.cache.FetchFeeds(a.config, a.archive, sources, nil)
			}

			twts = a.cache.GetByURL(req.URL)
		} else if a.db.HasUser(nick) {
			user, err := a.db.GetUser(nick)
			if err != nil {
				log.WithError(err).Errorf("error loading user object for %s", nick)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			profile = user.Profile(a.config.BaseURL, loggedInUser)
			twts = a.cache.GetByURL(profile.URI)
		} else if a.db.HasFeed(nick) {
			feed, err := a.db.GetFeed(nick)
			if err != nil {
				log.WithError(err).Errorf("error loading feed object for %s", nick)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			profile = feed.Profile(a.config.BaseURL, loggedInUser)

			twts = a.cache.GetByURL(profile.URI)
		} else {
			http.Error(w, "User/Feed not found", http.StatusNotFound)
			return
		}

		var pagedTwts types.Twts

		pager := paginator.New(adapter.NewSliceAdapter(twts), a.config.TwtsPerPage)
		pager.SetPage(req.Page)

		if err = pager.Results(&pagedTwts); err != nil {
			log.WithError(err).Error("error loading twts")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		res := types.PagedResponse{
			Twts: pagedTwts,
			Pager: types.PagerResponse{
				Current:   pager.Page(),
				MaxPages:  pager.PageNums(),
				TotalTwts: pager.Nums(),
			},
		}

		data, err := json.Marshal(res)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// ExternalProfileEndpoint ...
func (a *API) ExternalProfileEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		loggedInUser := a.getLoggedInUser(r)
		req, err := types.NewExternalProfileRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing external profile request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		uri := req.URL
		nick := req.Nick

		if uri == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		if !a.cache.IsCached(uri) {
			a.tasks.DispatchFunc(func() error {
				sources := make(types.FetchFeedRequests)
				sources[types.FetchFeedRequest{Nick: nick, URL: uri}] = true
				a.cache.FetchFeeds(a.config, a.archive, sources, nil)
				return nil
			})
		}

		var twter types.Twter

		if cachedTwter := a.cache.GetTwter(uri); cachedTwter != nil {
			twter = *cachedTwter
		} else {
			twter = types.Twter{Nick: nick, URI: uri}
		}

		// Set nick to what the user follows as (if any)
		nick = loggedInUser.FollowsAs(uri)

		// If no nick provided try to guess a suitable nick
		// from the feed or some heuristics from the feed's URI
		// (borrowed from Yarns)
		if nick == "" {
			if twter.Nick != "" {
				nick = twter.Nick
			} else {
				// TODO: Move this logic into types/lextwt and types/retwt
				if u, err := url.Parse(uri); err == nil {
					if strings.HasSuffix(u.Path, "/twtxt.txt") {
						if rest := strings.TrimSuffix(u.Path, "/twtxt.txt"); rest != "" {
							nick = strings.Trim(rest, "/")
						} else {
							nick = u.Hostname()
						}
					} else if strings.HasSuffix(u.Path, ".txt") {
						base := filepath.Base(u.Path)
						if name := strings.TrimSuffix(base, filepath.Ext(base)); name != "" {
							nick = name
						} else {
							nick = u.Hostname()
						}
					} else {
						nick = Slugify(uri)
					}
				}
			}
		}

		var follows types.Follows
		for nick, twter := range twter.Follow {
			follows = append(follows, types.Follow{Nick: nick, URI: twter.URI})
		}

		profile := types.Profile{
			Type: "External",

			Nick:        nick,
			Description: twter.Tagline,
			Avatar:      URLForExternalAvatar(a.config, uri),
			URI:         uri,

			Following:  follows,
			NFollowing: twter.Following,
			NFollowers: twter.Followers,

			ShowFollowing: true,
			ShowFollowers: true,

			Follows:    loggedInUser.Follows(uri),
			FollowedBy: loggedInUser.FollowedBy(uri),
			Muted:      loggedInUser.HasMuted(uri),
		}

		profileResponse := types.ProfileResponse{
			Profile: profile.AsOldProfile(),
			Twter:   twter,
		}

		data, err := json.Marshal(profileResponse)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// MuteEndpoint ...
func (a *API) MuteEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		user := r.Context().Value(UserContextKey).(*User)

		req, err := types.NewMuteRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing mute request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		nick := req.Nick
		url := req.URL

		if nick == "" || url == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		user.Mute(nick, url)

		a.cache.GetByUser(user, true)

		if err := a.db.SetUser(user.Username, user); err != nil {
			log.WithError(err).Error("error updating user object")
			http.Error(w, "User Update Failed", http.StatusInternalServerError)
			return
		}

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))

	}
}

// UnmuteEndpoint ...
func (a *API) UnmuteEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		user := r.Context().Value(UserContextKey).(*User)

		req, err := types.NewUnmuteRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing unmute request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		nick := req.Nick

		if nick == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		user.Unmute(nick)

		a.cache.GetByUser(user, true)

		if err := a.db.SetUser(user.Username, user); err != nil {
			log.WithError(err).Error("error updating user object")
			http.Error(w, "User Update Failed", http.StatusInternalServerError)
			return
		}

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

// SupportEndpoint ...
func (a *API) SupportEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		req, err := types.NewSupportRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing support request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		name := req.Name
		email := req.Email
		subject := req.Subject
		message := req.Message

		if err := SendSupportRequestEmail(a.config, name, email, subject, message); err != nil {
			log.WithError(err).Errorf("unable to send support email for %s", email)
			log.WithError(err).Error("error sending support request")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		log.Infof("support message email sent for %s", email)

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))

	}
}

// ReportEndpoint ...
func (a *API) ReportEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		req, err := types.NewReportRequest(r.Body)
		if err != nil {
			log.WithError(err).Error("error parsing report request")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		nick := req.Nick
		url := req.URL

		if nick == "" || url == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		name := req.Name
		email := req.Email
		category := req.Category
		message := req.Message

		if err := SendReportAbuseEmail(a.config, nick, url, name, email, category, message); err != nil {
			log.WithError(err).Errorf("unable to send report email for %s", email)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// No real response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

// PodConfigEndpoint ...
func (a *API) PodConfigEndpoint() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		data, err := json.Marshal(a.config.Settings())
		if err != nil {
			log.WithError(err).Error("error serializing pod config response")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// WebSubEndpoint ...
func (a *API) WebSubEndpoint() httprouter.Handle {
	isAdminUser := IsAdminUserFactory(a.config)

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		user := r.Context().Value(UserContextKey).(*User)

		if !isAdminUser(user) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		websub.DebugEndpoint(w, r)
	}
}
