// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"strings"
	"sync"
	"time"
)

const (
	// playbackLockTTL is how long a playback lock survives without being renewed.
	// It is three times the progress save interval (10s), so two lost requests are tolerated.
	playbackLockTTL = 30 * time.Second
	// playbackSweepAtSize triggers an opportunistic cleanup of expired locks.
	playbackSweepAtSize = 512
)

type playbackKey struct {
	userID      int64
	enclosureID int64
}

type playbackHolder struct {
	sessionID string
	device    string
	at        time.Time
}

// playbackRegistry keeps per-enclosure playback locks and the per-user remote
// pause sequence in memory. It is intentionally not persisted: the state is a
// best-effort coordination hint, and a restart simply frees every lock.
type playbackRegistry struct {
	mu       sync.Mutex
	holders  map[playbackKey]playbackHolder
	pauseSeq map[int64]uint64
	now      func() time.Time
}

func newPlaybackRegistry() *playbackRegistry {
	return &playbackRegistry{
		holders:  make(map[playbackKey]playbackHolder),
		pauseSeq: make(map[int64]uint64),
		now:      time.Now,
	}
}

// Claim grants the lock if it is free, expired, already owned by sessionID, or force is true.
func (r *playbackRegistry) Claim(userID, enclosureID int64, sessionID, device string, force bool) bool {
	if sessionID == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := playbackKey{userID: userID, enclosureID: enclosureID}
	if existing, found := r.holders[key]; found && !force {
		if existing.sessionID != sessionID && r.now().Sub(existing.at) <= playbackLockTTL {
			return false
		}
	}

	r.holders[key] = playbackHolder{sessionID: sessionID, device: device, at: r.now()}
	r.sweepLocked()
	return true
}

// Release drops the lock only if it is held by the given session.
func (r *playbackRegistry) Release(userID, enclosureID int64, sessionID string) {
	if sessionID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := playbackKey{userID: userID, enclosureID: enclosureID}
	if existing, found := r.holders[key]; found && existing.sessionID == sessionID {
		delete(r.holders, key)
	}
}

// State returns "self", "other", or "none", along with the holder device label.
func (r *playbackRegistry) State(userID, enclosureID int64, sessionID string) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := playbackKey{userID: userID, enclosureID: enclosureID}
	existing, found := r.holders[key]
	if !found {
		return "none", ""
	}

	if r.now().Sub(existing.at) > playbackLockTTL {
		delete(r.holders, key)
		return "none", ""
	}

	if existing.sessionID == sessionID {
		return "self", existing.device
	}

	return "other", existing.device
}

// PauseAll bumps the remote pause sequence of the user and frees all of their locks.
func (r *playbackRegistry) PauseAll(userID int64) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pauseSeq[userID]++
	for key := range r.holders {
		if key.userID == userID {
			delete(r.holders, key)
		}
	}

	return r.pauseSeq[userID]
}

// PauseSeq returns the last remote pause sequence of the user (0 means never requested).
func (r *playbackRegistry) PauseSeq(userID int64) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.pauseSeq[userID]
}

func (r *playbackRegistry) sweepLocked() {
	if len(r.holders) <= playbackSweepAtSize {
		return
	}

	now := r.now()
	for key, holder := range r.holders {
		if now.Sub(holder.at) > playbackLockTTL {
			delete(r.holders, key)
		}
	}
}

// playbackDeviceLabel turns a User-Agent into a short human readable device label.
func playbackDeviceLabel(userAgent string) string {
	var platform string
	switch {
	case strings.Contains(userAgent, "iPhone"):
		platform = "iPhone"
	case strings.Contains(userAgent, "iPad"):
		platform = "iPad"
	case strings.Contains(userAgent, "Android"):
		platform = "Android"
	case strings.Contains(userAgent, "Macintosh"):
		platform = "Mac"
	case strings.Contains(userAgent, "Windows"):
		platform = "Windows"
	case strings.Contains(userAgent, "Linux"):
		platform = "Linux"
	}

	var browser string
	switch {
	case strings.Contains(userAgent, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(userAgent, "Edg/"):
		browser = "Edge"
	case strings.Contains(userAgent, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(userAgent, "Safari/"):
		browser = "Safari"
	}

	switch {
	case platform != "" && browser != "":
		return platform + " · " + browser
	case platform != "":
		return platform
	case browser != "":
		return browser
	}

	return "Web"
}
