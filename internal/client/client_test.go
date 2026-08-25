// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"garmin-connect-workout-cli/internal/config"
)

func TestTruncateBody(t *testing.T) {
	t.Parallel()

	const maxBytes = 4096

	cases := []struct {
		name        string
		input       []byte
		wantLen     int
		wantHasTail bool
	}{
		{"empty", nil, 0, false},
		{"under cap", []byte("hello"), 5, false},
		{"at cap", bytes.Repeat([]byte("a"), maxBytes), maxBytes, false},
		{"one over cap", bytes.Repeat([]byte("a"), maxBytes+1), maxBytes + 3, true},
		{"huge body", bytes.Repeat([]byte("a"), maxBytes*8), maxBytes + 3, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncateBody(tc.input)
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.wantHasTail && !strings.HasSuffix(got, "...") {
				t.Fatalf("want trailing %q", "...")
			}
			if !tc.wantHasTail && strings.HasSuffix(got, "...") {
				t.Fatalf("unexpected trailing %q in %q", "...", got)
			}
		})
	}
}

func TestTruncateBody_UTF8RuneAtBoundary(t *testing.T) {
	t.Parallel()

	// '€' is 3 bytes (0xE2 0x82 0xAC). Place it so the slice at byte 4096 cuts
	// mid-rune; strings.ToValidUTF8 should drop the partial rune cleanly rather
	// than emit U+FFFD or invalid UTF-8.
	prefix := strings.Repeat("a", 4094)
	body := []byte(prefix + "€" + strings.Repeat("b", 100))
	got := truncateBody(body)

	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8")
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("output contains replacement rune U+FFFD")
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("want trailing %q", "...")
	}
	// Partial rune must be dropped, not replaced: 4094 valid bytes + "...".
	if want := 4094 + 3; len(got) != want {
		t.Fatalf("len = %d, want %d (partial rune should be dropped, not replaced)", len(got), want)
	}
}

func TestMutatingRequestDoesNotRetry429(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL, AuthHeaderVal: "Bearer test"}, time.Second, 100)
	_, status, err := c.Post(context.Background(), "/workout-service/workout", map[string]any{"name": "test"})
	if err == nil || status != http.StatusTooManyRequests {
		t.Fatalf("Post() status = %d, error = %v", status, err)
	}
	if calls != 1 {
		t.Fatalf("POST calls = %d, want 1", calls)
	}
}
