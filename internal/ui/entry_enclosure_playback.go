// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	json_parser "encoding/json"
	"errors"
	"io"
	"net/http"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/model"
)

// playbackTarget resolves the enclosure and the web session identity used as playback owner.
func (h *handler) playbackTarget(w http.ResponseWriter, r *http.Request) (*model.Enclosure, string, string, bool) {
	enclosure, err := h.store.EnclosureByID(request.UserID(r), request.RouteInt64Param(r, "enclosureID"))
	if err != nil {
		response.JSONServerError(w, r, err)
		return nil, "", "", false
	}

	if enclosure == nil {
		response.JSONNotFound(w, r)
		return nil, "", "", false
	}

	session := request.WebSession(r)
	if session == nil {
		return enclosure, "", "", true
	}

	return enclosure, session.ID, playbackDeviceLabel(session.UserAgent), true
}

func (h *handler) claimEnclosurePlayback(w http.ResponseWriter, r *http.Request) {
	enclosure, sessionID, device, ok := h.playbackTarget(w, r)
	if !ok {
		return
	}

	type playbackClaimRequest struct {
		Force bool `json:"force"`
	}

	var claimRequest playbackClaimRequest
	if err := json_parser.NewDecoder(r.Body).Decode(&claimRequest); err != nil && !errors.Is(err, io.EOF) {
		response.JSONBadRequest(w, r, err)
		return
	}

	granted := h.playback.Claim(enclosure.UserID, enclosure.ID, sessionID, device, claimRequest.Force)
	_, holderDevice := h.playback.State(enclosure.UserID, enclosure.ID, sessionID)

	response.JSON(w, r, map[string]any{
		"granted":   granted,
		"device":    holderDevice,
		"position":  enclosure.MediaProgression,
		"pause_seq": h.playback.PauseSeq(enclosure.UserID),
	})
}

func (h *handler) releaseEnclosurePlayback(w http.ResponseWriter, r *http.Request) {
	enclosure, sessionID, _, ok := h.playbackTarget(w, r)
	if !ok {
		return
	}

	h.playback.Release(enclosure.UserID, enclosure.ID, sessionID)
	response.NoContent(w, r)
}

func (h *handler) enclosurePlaybackState(w http.ResponseWriter, r *http.Request) {
	enclosure, sessionID, _, ok := h.playbackTarget(w, r)
	if !ok {
		return
	}

	owner, device := h.playback.State(enclosure.UserID, enclosure.ID, sessionID)

	response.JSON(w, r, map[string]any{
		"owner":     owner,
		"device":    device,
		"position":  enclosure.MediaProgression,
		"pause_seq": h.playback.PauseSeq(enclosure.UserID),
	})
}
