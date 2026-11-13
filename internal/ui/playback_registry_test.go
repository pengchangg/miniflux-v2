// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"sync"
	"testing"
	"time"
)

// newTestPlaybackRegistry returns a registry with a controllable clock.
func newTestPlaybackRegistry() (*playbackRegistry, *time.Time) {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	registry := newPlaybackRegistry()
	registry.now = func() time.Time { return current }
	return registry, &current
}

func TestPlaybackRegistryClaimIsExclusive(t *testing.T) {
	registry, _ := newTestPlaybackRegistry()

	var wg sync.WaitGroup
	granted := make(chan string, 32)
	for i := 0; i < cap(granted); i++ {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			if registry.Claim(1, 10, sessionID, "device", false) {
				granted <- sessionID
			}
		}("session-" + string(rune('a'+i)))
	}
	wg.Wait()
	close(granted)

	if count := len(granted); count != 1 {
		t.Fatalf(`expected exactly one winner, got %d`, count)
	}
}

func TestPlaybackRegistryClaimRenewsAndBlocksOthers(t *testing.T) {
	registry, current := newTestPlaybackRegistry()

	if !registry.Claim(1, 10, "a", "iPhone", false) {
		t.Fatal(`first claim should be granted`)
	}
	*current = current.Add(20 * time.Second)
	if !registry.Claim(1, 10, "a", "iPhone", false) {
		t.Fatal(`the holder should be able to renew`)
	}
	if registry.Claim(1, 10, "b", "iPad", false) {
		t.Fatal(`another session should be rejected while the lock is live`)
	}
	if !registry.Claim(1, 10, "b", "iPad", true) {
		t.Fatal(`a forced claim should always win`)
	}
}

func TestPlaybackRegistryClaimAfterExpiry(t *testing.T) {
	registry, current := newTestPlaybackRegistry()

	if !registry.Claim(1, 10, "a", "iPhone", false) {
		t.Fatal(`first claim should be granted`)
	}
	*current = current.Add(playbackLockTTL + time.Second)
	if !registry.Claim(1, 10, "b", "iPad", false) {
		t.Fatal(`an expired lock should be stealable`)
	}
}

func TestPlaybackRegistryClaimWithoutSession(t *testing.T) {
	registry, _ := newTestPlaybackRegistry()

	if registry.Claim(1, 10, "", "Web", false) {
		t.Fatal(`an empty session ID must never hold a lock`)
	}
}

func TestPlaybackRegistryReleaseOnlyOwnLock(t *testing.T) {
	registry, _ := newTestPlaybackRegistry()

	registry.Claim(1, 10, "a", "iPhone", false)
	registry.Release(1, 10, "b")
	if registry.Claim(1, 10, "b", "iPad", false) {
		t.Fatal(`releasing from another session must not free the lock`)
	}

	registry.Claim(1, 10, "a", "iPhone", false)
	registry.Release(1, 10, "a")
	if !registry.Claim(1, 10, "b", "iPad", false) {
		t.Fatal(`the lock should be free after the holder released it`)
	}
}

func TestPlaybackRegistryState(t *testing.T) {
	registry, current := newTestPlaybackRegistry()

	if state, _ := registry.State(1, 10, "a"); state != "none" {
		t.Fatalf(`expected "none", got %q`, state)
	}

	registry.Claim(1, 10, "a", "iPhone", false)
	if state, device := registry.State(1, 10, "a"); state != "self" || device != "iPhone" {
		t.Fatalf(`expected "self"/"iPhone", got %q/%q`, state, device)
	}
	if state, device := registry.State(1, 10, "b"); state != "other" || device != "iPhone" {
		t.Fatalf(`expected "other"/"iPhone", got %q/%q`, state, device)
	}

	*current = current.Add(playbackLockTTL + time.Second)
	if state, _ := registry.State(1, 10, "b"); state != "none" {
		t.Fatalf(`expected "none" after expiry, got %q`, state)
	}
}

func TestPlaybackRegistryPauseAll(t *testing.T) {
	registry, _ := newTestPlaybackRegistry()

	registry.Claim(1, 10, "a", "iPhone", false)
	registry.Claim(1, 11, "a", "iPhone", false)
	registry.Claim(2, 10, "c", "iPad", false)

	if seq := registry.PauseAll(1); seq != 1 {
		t.Fatalf(`expected pause sequence 1, got %d`, seq)
	}
	if seq := registry.PauseSeq(1); seq != 1 {
		t.Fatalf(`expected pause sequence 1, got %d`, seq)
	}
	if seq := registry.PauseSeq(2); seq != 0 {
		t.Fatalf(`pause sequence of another user should stay 0, got %d`, seq)
	}

	if state, _ := registry.State(1, 10, "a"); state != "none" {
		t.Fatalf(`expected the user locks to be cleared, got %q`, state)
	}
	if state, _ := registry.State(2, 10, "c"); state != "self" {
		t.Fatalf(`locks of other users must be untouched, got %q`, state)
	}

	if seq := registry.PauseAll(1); seq != 2 {
		t.Fatalf(`expected pause sequence 2, got %d`, seq)
	}
}

func TestPlaybackRegistrySweep(t *testing.T) {
	registry, current := newTestPlaybackRegistry()

	for i := 0; i < playbackSweepAtSize+10; i++ {
		registry.Claim(int64(i), 10, "session", "device", false)
	}
	*current = current.Add(playbackLockTTL + time.Second)
	registry.Claim(9999, 10, "session", "device", false)

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if len(registry.holders) > playbackSweepAtSize {
		t.Fatalf(`expired locks should have been swept, got %d entries`, len(registry.holders))
	}
}

func TestPlaybackDeviceLabel(t *testing.T) {
	scenarios := map[string]string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1": "iPhone · Safari",
		"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36":                            "Android · Chrome",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0":           "Windows · Edge",
		"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0":                                                                  "Linux · Firefox",
		"": "Web",
	}

	for userAgent, expected := range scenarios {
		if label := playbackDeviceLabel(userAgent); label != expected {
			t.Errorf(`expected %q for %q, got %q`, expected, userAgent, label)
		}
	}
}
