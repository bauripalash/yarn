// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

func (s *Server) LinkVerification() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		ctx := NewContext(s, r)

		uri := r.URL.Query().Get("uri")

    if uri == "" {
      ctx.Error = true
      ctx.Message = s.tr(ctx, "LinkVerifyNoURL")
      s.render("error", w, ctx)
      return
    }

		permitted, _ := s.config.PermittedImage(NormalizeURL(uri))
		instanceName := s.config.Name

		if (!permitted) && (!ctx.Authenticated || ctx.User.LinkVerification) {
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
			s.render("prompt", w, ctx)
		}

    http.Redirect(w, r, uri, http.StatusFound)
    return
	}
}
