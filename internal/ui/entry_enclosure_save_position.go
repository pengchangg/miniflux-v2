// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	json_parser "encoding/json"
	"net/http"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
)

func (h *handler) saveEnclosureProgression(w http.ResponseWriter, r *http.Request) {
	enclosureID := request.RouteInt64Param(r, "enclosureID")
	enclosure, err := h.store.EnclosureByID(request.UserID(r), enclosureID)
	if err != nil {
		response.JSONServerError(w, r, err)
		return
	}

	if enclosure == nil {
		response.JSONNotFound(w, r)
		return
	}

	type enclosurePositionSaveRequest struct {
		Progression int64 `json:"progression"`
	}

	var postData enclosurePositionSaveRequest
	if err := json_parser.NewDecoder(r.Body).Decode(&postData); err != nil {
		response.JSONServerError(w, r, err)
		return
	}

	session := request.WebSession(r)
	var sessionID, device string
	if session != nil {
		sessionID = session.ID
		device = playbackDeviceLabel(session.UserAgent)
	}

	pauseSeq := h.playback.PauseSeq(enclosure.UserID)
	if !h.playback.Claim(enclosure.UserID, enclosure.ID, sessionID, device, false) {
		// The lock is held by another device: skip the write so progress is not overwritten at random.
		response.JSON(w, r, map[string]any{"granted": false, "pause_seq": pauseSeq})
		return
	}

	enclosure.MediaProgression = postData.Progression

	if err := h.store.UpdateEnclosure(enclosure); err != nil {
		response.JSONServerError(w, r, err)
		return
	}

	response.JSONCreated(w, r, map[string]any{"message": "saved", "granted": true, "pause_seq": pauseSeq})
}
