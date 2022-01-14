package internal

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"git.mills.io/yarnsocial/yarn/types"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

// PostHandler handles the creation/modification/deletion of a twt.
//
// TODO: Support deleting/patching last feed (`postas`) twt too.
func (s *Server) PostHandler() httprouter.Handle {
	var appendTwt = AppendTwtFactory(s.config, s.db)
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		postAs := strings.ToLower(strings.TrimSpace(r.FormValue("postas")))
		ctx := NewContext(s, r)
		var err error

		// Validate twt user.
		if ctx.User.Username == "" {
			log.Errorf("error loading user object for %s", ctx.Username)
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorPostingTwt")
			s.render("error", w, ctx)
			return
		}

		defer s.cache.DeleteUserViews(ctx.User)

		hash := r.FormValue("hash")
		var lastTwt types.Twt

		// If we are deleting the last twt, delete it and return.
		// Else, we are editing the last twt; so, delete it and continue.
		if r.Method == http.MethodDelete || hash != "" {
			// Retrieve the last twt.
			lastTwt, _, err = GetLastTwt(s.config, ctx.User)
			if err != nil {
				ctx.Error = true
				ctx.Message = s.tr(ctx, "ErrorPostingTwt")
				s.render("error", w, ctx)
				return
			}

			// If hash != "", the user either wants to delete or edit
			// the last twt; therefore, it should correspond to its hash.
			if hash != "" && lastTwt.Hash() != hash {
				ctx.Error = true
				ctx.Message = s.tr(ctx, "ErrorDeleteLastTwt")
				s.render("error", w, ctx)
				return
			}

			// Delete the last twt from persistent memory.
			if err = DeleteLastTwt(s.config, ctx.User); err != nil {
				ctx.Error = true
				ctx.Message = s.tr(ctx, "ErrorDeleteLastTwt")
				s.render("error", w, ctx)
				return
			}

			// Snipe the last twt from feeds.
			s.cache.SnipeFeed(lastTwt.Twter().URL, lastTwt)
			for feed := range ctx.User.Source() {
				s.cache.SnipeFeed(feed.URL, lastTwt)
			}

			// If we are simply deleting the last twt, we have no need to proceed
			// further.
			if r.Method == http.MethodDelete {
				return
			}
		}

		//
		// Post a new twt.
		//

		// Validate twt text.
		text := CleanTwt(r.FormValue("text"))
		if text == "" {
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorNoPostContent")
			s.render("error", w, ctx)
			return
		}

		// Validate twt reply into twt text.
		reply := strings.TrimSpace(r.FormValue("reply"))
		if reply != "" {
			re := regexp.MustCompile(`^(@<.*>[, ]*)*(\(.*?\))(.*)`)
			match := re.FindStringSubmatch(text)
			if match == nil {
				text = fmt.Sprintf("(%s) %s", reply, text)
			}
		}

		var (
			twt     types.Twt = types.NilTwt
			feedURL string
		)

		// Post the twt.
		switch postAs {
		case "", ctx.User.Username:
			feedURL = s.config.URLForUser(ctx.User.Username)

			if hash != "" && lastTwt.Hash() == hash {
				twt, err = appendTwt(ctx.User, nil, text, lastTwt.Created())
			} else {
				twt, err = appendTwt(ctx.User, nil, text)
			}
		default:
			if ctx.User.OwnsFeed(postAs) {
				feed, feedErr := s.db.GetFeed(postAs)
				if feedErr != nil {
					log.WithError(err).Error("error loading feed object")
					ctx.Error = true
					ctx.Message = s.tr(ctx, "ErrorPostingTwt")
					s.render("error", w, ctx)
					return
				}

				feedURL = s.config.URLForUser(postAs)
				if hash != "" && lastTwt.Hash() == hash {
					twt, err = appendTwt(ctx.User, feed, text, lastTwt.Created)
				} else {
					twt, err = appendTwt(ctx.User, feed, text)
				}
			} else {
				err = ErrFeedImposter
			}
		}

		if err != nil {
			log.WithError(err).Error("error posting twt")
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorPostingTwt")
			s.render("error", w, ctx)
			return
		}

		// Update user's own timeline with their own new post.
		s.cache.InjectFeed(feedURL, twt)

		// Refresh user views.
		s.cache.GetByUser(ctx.User, true)

		// WebMentions ...
		// TODO: Use a queue here instead?
		// TODO: Fix Webmentions
		// TODO: https://git.mills.io/yarnsocial/yarn/issues/438
		// TODO: https://git.mills.io/yarnsocial/yarn/issues/515
		/*
			if _, err := s.tasks.Dispatch(NewFuncTask(func() error {
				for _, m := range twt.Mentions() {
					twter := m.Twter()
					if !isLocalURL(twter.RequestURI) {
						if err := WebMention(twter.RequestURI, URLForTwt(s.config.BaseURL, twt.Hash())); err != nil {
							log.WithError(err).Warnf("error sending webmention to %s", twter.RequestURI)
						}
					}
				}
				return nil
			})); err != nil {
				log.WithError(err).Warn("error submitting task for webmentions")
			}
		*/

		http.Redirect(w, r, RedirectRefererURL(r, s.config, "/"), http.StatusFound)
	}
}
