// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGarminMutationSessionDiscoversRouteWithoutMutating(t *testing.T) {
	var calls []string
	session := testGarminMutationSession(func(_ context.Context, base, method, path string, _ any) (browserPostResponse, error) {
		calls = append(calls, fmt.Sprintf("%s %s%s", method, base, path))
		if base == "a" {
			return browserPostResponse{BaseURL: base, Status: 427, Body: `{"error":{"status-code":"427"}}`}, nil
		}
		return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
	})

	if err := session.discoverBase(); err != nil {
		t.Fatalf("discoverBase() error = %v", err)
	}
	if session.base != "b" {
		t.Fatalf("base = %q, want b", session.base)
	}
	if len(calls) != 2 || calls[0][:3] != "GET" || calls[1][:3] != "GET" {
		t.Fatalf("calls = %#v, want GET-only discovery", calls)
	}
}

func TestGarminMutationSessionVerifiesBefore427Fallback(t *testing.T) {
	var postBases []string
	session := testGarminMutationSession(func(_ context.Context, base, method, path string, _ any) (browserPostResponse, error) {
		if method == "GET" {
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		}
		postBases = append(postBases, base)
		if base == "a" {
			return browserPostResponse{BaseURL: base, Status: 427, Body: `{"error":{"status-code":"427"}}`}, nil
		}
		return browserPostResponse{BaseURL: base, Status: 200, Body: `{"workoutId":"42"}`}, nil
	})
	verifyCalls := 0
	response, err := session.mutate("POST", "/workout-service/workout", map[string]any{"name": "A"}, func() (browserPostResponse, bool, error) {
		verifyCalls++
		if _, err := session.read("/workout-service/workouts?start=0&limit=100"); err != nil {
			return browserPostResponse{}, false, err
		}
		return browserPostResponse{}, false, nil
	})
	if err != nil {
		t.Fatalf("mutate() error = %v", err)
	}
	if response.Status != 200 || verifyCalls != 1 {
		t.Fatalf("response = %#v, verifyCalls = %d", response, verifyCalls)
	}
	if len(postBases) != 2 || postBases[0] != "a" || postBases[1] != "b" {
		t.Fatalf("POST bases = %#v, want [a b]", postBases)
	}
}

func TestGarminMutationSessionRecoversVerifiedMutationAfter427(t *testing.T) {
	postCalls := 0
	successCalls := 0
	session := testGarminMutationSession(func(_ context.Context, base, method, _ string, _ any) (browserPostResponse, error) {
		if method == "GET" {
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		}
		postCalls++
		return browserPostResponse{BaseURL: base, Status: 427, Body: `{"error":{"status-code":"427"}}`}, nil
	})
	session.onSuccess = func() {
		successCalls++
	}
	recovered := browserPostResponse{BaseURL: "verified", Status: 200, Body: `{"workoutId":"42"}`}
	response, err := session.mutate("POST", "/workout-service/workout", nil, func() (browserPostResponse, bool, error) {
		return recovered, true, nil
	})
	if err != nil {
		t.Fatalf("mutate() error = %v", err)
	}
	if response.Body != recovered.Body || postCalls != 1 {
		t.Fatalf("response = %#v, postCalls = %d", response, postCalls)
	}
	if successCalls != 1 {
		t.Fatalf("success calls = %d, want 1", successCalls)
	}
}

func TestGarminMutationSessionOpensCircuitOn429(t *testing.T) {
	evaluateCalls := 0
	rateLimitCalls := 0
	session := testGarminMutationSession(func(_ context.Context, base, method, _ string, _ any) (browserPostResponse, error) {
		evaluateCalls++
		if method == "GET" {
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		}
		return browserPostResponse{BaseURL: base, Status: 429, Body: `{"retry_after":30}`}, nil
	})
	session.onRateLimit = func(string) error {
		rateLimitCalls++
		return nil
	}
	if _, err := session.mutate("POST", "/workout-service/workout", nil, nil); err == nil {
		t.Fatal("first mutate() error = nil, want rate-limit error")
	}
	firstCallCount := evaluateCalls
	if _, err := session.mutate("POST", "/workout-service/workout", nil, nil); err == nil {
		t.Fatal("second mutate() error = nil, want open-circuit error")
	}
	if evaluateCalls != firstCallCount || rateLimitCalls != 1 {
		t.Fatalf("evaluateCalls = %d, rateLimitCalls = %d", evaluateCalls, rateLimitCalls)
	}
}

func TestGarminMutationSessionSpacesEveryRequest(t *testing.T) {
	now := time.Date(2026, 7, 28, 3, 0, 0, 0, time.UTC)
	var waits []time.Duration
	session := testGarminMutationSession(func(_ context.Context, base, _ string, _ string, _ any) (browserPostResponse, error) {
		return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
	})
	session.requestDelay = 2 * time.Second
	session.now = func() time.Time { return now }
	session.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		now = now.Add(delay)
		return nil
	}

	if err := session.discoverBase(); err != nil {
		t.Fatalf("discoverBase() error = %v", err)
	}
	if _, err := session.read("/calendar-service/year/2026/month/6"); err != nil {
		t.Fatalf("read() error = %v", err)
	}
	if _, err := session.mutate("POST", "/workout-service/workout", nil, nil); err != nil {
		t.Fatalf("mutate() error = %v", err)
	}
	if len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 2*time.Second {
		t.Fatalf("waits = %#v, want [2s 2s]", waits)
	}
}

func testGarminMutationSession(evaluate garminBrowserEvaluateFunc) *garminBrowserMutationSession {
	return &garminBrowserMutationSession{
		ctx:         context.Background(),
		bases:       []string{"a", "b"},
		evaluate:    evaluate,
		onRateLimit: func(string) error { return nil },
		onSuccess:   func() {},
		now:         time.Now,
		wait:        waitForGarminMutationSpacing,
		blocked:     map[string]bool{},
	}
}
