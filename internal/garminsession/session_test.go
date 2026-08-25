// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package garminsession

import (
	"testing"
	"time"
)

func TestCookieOnlySessionRequiresSuccessfulVerification(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	legacy := &Session{Cookie: "session=legacy", CapturedAt: now}
	if legacy.Active(now) {
		t.Fatal("legacy cookie-only session reported active without verified_at")
	}
	verified := &Session{Cookie: "session=verified", CapturedAt: now, VerifiedAt: now}
	if !verified.Active(now) {
		t.Fatal("verified cookie-only session reported inactive")
	}
}

func TestStructuredCookiesRoundTrip(t *testing.T) {
	t.Setenv("GARMIN_CONNECT_WORKOUTS_DATA_DIR", t.TempDir())
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	want := Session{
		Cookie:     "session=secret",
		CapturedAt: now,
		VerifiedAt: now,
		Cookies: []Cookie{{
			Name:     "session",
			Value:    "secret",
			Domain:   ".connect.garmin.com",
			Path:     "/",
			Expires:  float64(now.Add(time.Hour).Unix()),
			HTTPOnly: true,
			Secure:   true,
			SameSite: "Lax",
		}},
	}
	if _, err := Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, _, found, err := Load()
	if err != nil || !found {
		t.Fatalf("Load() found = %v, error = %v", found, err)
	}
	if len(got.Cookies) != 1 || got.Cookies[0] != want.Cookies[0] || !got.VerifiedAt.Equal(now) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}
