// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

// MuteHandler ...
func (s *Server) MuteHandler() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		ctx := NewContext(s, r)

		nick := strings.TrimSpace(r.FormValue("nick"))
		url := NormalizeURL(r.FormValue("url"))
		hash := p.ByName("hash")

		if hash == "" && (nick == "" || url == "") {
			ctx.Error = true
			ctx.Message = "At least nick + url or hash must be specified"
			s.render("error", w, ctx)
			return
		}

		user := ctx.User
		if user == nil {
			log.Fatalf("user not found in context")
			return
		}

		if nick != "" && url != "" {
			user.Mute(nick, NormalizeURL(url))
		} else if hash != "" {
			user.Mute(fmt.Sprintf("twt:%s", hash), hash)
			user.Mute(fmt.Sprintf("yarn:%s", hash), fmt.Sprintf("(#%s)", hash))
		}

		if err := s.db.SetUser(ctx.Username, user); err != nil {
			ctx.Error = true
			ctx.Message = fmt.Sprintf("Error muting feed %s: %s", nick, url)
			s.render("error", w, ctx)
			return
		}

		s.cache.DeleteUserViews(ctx.User)

		ctx.Error = false
		if hash != "" {
			ctx.Message = fmt.Sprintf("Successfully muted Twt (and its reeplies) %s", hash)
		} else {
			ctx.Message = fmt.Sprintf("Successfully muted %s: %s", nick, url)
		}
		s.render("error", w, ctx)
	}
}

// UnmuteHandler ...
func (s *Server) UnmuteHandler() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		ctx := NewContext(s, r)

		nick := strings.TrimSpace(r.FormValue("nick"))
		hash := p.ByName("hash")

		if nick == "" && hash == "" {
			ctx.Error = true
			ctx.Message = "No nick or hash specified to unmute"
			s.render("error", w, ctx)
			return
		}

		user := ctx.User
		if user == nil {
			log.Fatalf("user not found in context")
		}

		if nick != "" {
			user.Unmute(nick)
		} else if hash != "" {
			user.Unmute(fmt.Sprintf("twt:%s", hash))
			user.Unmute(fmt.Sprintf("yarn:%s", hash))
		}

		if err := s.db.SetUser(ctx.Username, user); err != nil {
			ctx.Error = true
			ctx.Message = fmt.Sprintf("Error unmuting feed %s", nick)
			s.render("error", w, ctx)
			return
		}

		s.cache.DeleteUserViews(ctx.User)

		ctx.Error = false
		if hash != "" {
			ctx.Message = fmt.Sprintf("Successfully unmuted Twt (and its replies) %s", hash)
		} else {
			ctx.Message = fmt.Sprintf("Successfully unmuted %s", nick)
		}
		s.render("error", w, ctx)
	}
}
