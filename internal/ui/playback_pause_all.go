// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"net/http"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/locale"
)

// pauseAllPlayback asks every client of the current user to stop playing.
func (h *handler) pauseAllPlayback(w http.ResponseWriter, r *http.Request) {
	h.playback.PauseAll(request.UserID(r))

	sess := request.WebSession(r)
	sess.SetSuccessMessage(locale.NewPrinter(sess.Language()).Print("alert.playback_paused"))

	response.HTMLRedirect(w, r, h.routePath("/sessions"))
}
