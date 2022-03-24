// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"fmt"
	"net/http"
  "regexp"
	"strings"

	"github.com/julienschmidt/httprouter"
)

func (s *Server) LinkVerification() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		ctx := NewContext(s, r)

		conf := s.config
		user := ctx.User

		uri := r.URL.Query().Get("uri")

    if uri == "" {
      ctx.Error = true
      ctx.Message = s.tr(ctx, "LinkVerifyNoURL")
      s.render("error", w, ctx)
      return
    }

		openLinksIn := conf.OpenLinksInPreference
		if user != nil {
			openLinksIn = user.OpenLinksInPreference
		}

		target := ""
		if strings.ToLower(openLinksIn) == "newwindow" {
		  target = "_blank"
		}

		permitted, _ := conf.PermittedImage(NormalizeURL(uri))
		instanceName := conf.Name

		if (!permitted) && (!ctx.Authenticated || user.LinkVerification) {
			trdata := map[string]interface{}{}
			trdata["InstanceName"] = instanceName

			ctx.PromptTitle = s.tr(ctx, "LinkVerifyTitle")
			ctx.PromptMessage = fmt.Sprintf(
				"%s<div id='verifyLink'>%s</div>",
				s.tr(ctx, "LinkVerifyMessage", trdata), NormalizeURL(uri),
			)
			ctx.PromptCallback = fmt.Sprintf(
				"%s", NormalizeURL(uri),
			)
			ctx.PromptApprove = s.tr(ctx, "LinkVerifyApprove")
			ctx.PromptCancel = s.tr(ctx, "LinkVerifyCancel")
			ctx.PromptTarget = target

			s.render("prompt", w, ctx)
		}

    http.Redirect(w, r, uri, http.StatusFound)
    return
	}
}

func StripTrackingParams(uri string) string {
	var param []string = []string{
		"dclid",
		"fbclid",
		"gclid",
		"gclsrc",
		"mkt_tok",
		"sc_campaign",
		"sc_category",
		"sc_channel",
		"sc_content",
		"sc_country",
		"sc_funnel",
		"sc_medium",
		"sc_publisher",
		"sc_segment",
		"utm_campaign",
		"utm_content",
		"utm_medium",
		"utm_source",
		"utm_term",
	}

	for _, p := range param {
		re, _ := regexp.Compile(`\??` + p + `=.+&{1}`)
		uri = re.ReplaceAllString(uri, "")
	}

	return uri
}
