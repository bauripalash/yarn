// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

// RegisterHandler ...
func (s *Server) RegisterHandler() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		ctx := NewContext(s, r)

		if r.Method == "GET" {
			if s.config.OpenRegistrations {
				s.render("register", w, ctx)
			} else {
				ctx.Error = true
				ctx.Message = s.tr(ctx, "ErrorRegisterDisabled")
				s.render("error", w, ctx)
			}

			return
		}

		username := NormalizeUsername(r.FormValue("username"))
		password := r.FormValue("password")
		// XXX: We DO NOT store this! (EVER)
		email := strings.TrimSpace(r.FormValue("email"))

		if err := ValidateUsername(username); err != nil {
			ctx.Error = true
			trdata := map[string]interface{}{
				"Error": err.Error(),
			}
			ctx.Message = s.tr(ctx, "ErrorValidateUsername", trdata)
			s.render("error", w, ctx)
			return
		}

		if s.db.HasUser(username) || s.db.HasFeed(username) {
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorHasUserOrFeed")
			s.render("error", w, ctx)
			return
		}

		p := filepath.Join(s.config.Data, feedsDir)
		if err := os.MkdirAll(p, 0755); err != nil {
			log.WithError(err).Error("error creating feeds directory")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fn := filepath.Join(p, username)
		if _, err := os.Stat(fn); err == nil {
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorUsernameExists")
			s.render("error", w, ctx)
			return
		}

		if err := ioutil.WriteFile(fn, []byte{}, 0644); err != nil {
			log.WithError(err).Error("error creating new user feed")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hash, err := s.pm.CreatePassword(password)
		if err != nil {
			log.WithError(err).Error("error creating password hash")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		recoveryHash := fmt.Sprintf("email:%s", FastHashString(email))

		user := NewUser()
		user.Username = username
		user.Password = hash
		user.Recovery = recoveryHash
		user.URL = URLForUser(s.config.BaseURL, username)
		user.CreatedAt = time.Now()

		// Default Feeds
		user.Follow(newsSpecialUser, s.config.URLForUser(newsSpecialUser))
		user.Follow(supportSpecialUser, s.config.URLForUser(supportSpecialUser))

		if err := s.db.SetUser(username, user); err != nil {
			log.WithError(err).Error("error saving user object for new user")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		//
		// Onboarding: Welcome new User and notify Poderator
		//

		s.tasks.DispatchFunc(func() error {
			if err := SendNewUserEmail(s.config, user.Username); err != nil {
				log.WithError(err).Warnf("error notifying admin of new user %s", user.Username)
				return err
			}
			return nil
		})
		s.tasks.DispatchFunc(func() error {
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
			welcomeText := CleanTwt(
				fmt.Sprintf(
					"👋 Hello @<%s %s>, welcome to %s, a [Yarn.social](https://yarn.social) Pod! To get started you may want to check out the pod's [Discover](/discover) feed to find users to follow and interact with. To follow new users, use the `⨁ Follow` button on their profile page or use the [Follow](/follow) form and enter a Twtxt URL. You may also find other feeds of interest via [Feeds](/feeds). Welcome! 🤗",
					user.Username, s.config.URLForUser(user.Username),
					s.config.Name,
				),
			)
			welcomeTwt, err := s.AppendTwt(adminUser, supportFeed, welcomeText)
			if err != nil {
				log.WithError(err).Warnf("error posting welcome for %s", user.Username)
				return err
			}
			s.cache.InjectFeed(s.config.URLForUser(supportFeed.Name), welcomeTwt)
			s.cache.DeleteUserViews(user)

			return nil
		})

		http.Redirect(w, r, "/login", http.StatusFound)
	}
}
