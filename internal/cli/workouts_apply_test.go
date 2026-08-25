// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"garmin-connect-workout-cli/internal/config"
	"garmin-connect-workout-cli/internal/garminsession"
	"garmin-connect-workout-cli/internal/workoutdraft"
)

func TestHasGarminWriteAuth(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{name: "nil config", cfg: nil, want: false},
		{name: "empty config", cfg: &config.Config{}, want: false},
		{name: "oauth header", cfg: &config.Config{AuthHeaderVal: "Bearer token"}, want: true},
		{name: "cookie-only session uses browser write", cfg: &config.Config{Headers: map[string]string{"Cookie": "SESSIONID=abc"}}, want: false},
		{name: "blank cookie header", cfg: &config.Config{Headers: map[string]string{"Cookie": "  "}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasGarminWriteAuth(tt.cfg); got != tt.want {
				t.Fatalf("hasGarminWriteAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUseGarminBrowserMutationSession(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{name: "nil config", cfg: nil, want: true},
		{name: "empty config", cfg: &config.Config{}, want: true},
		{name: "direct oauth header", cfg: &config.Config{AuthHeaderVal: "Bearer token", AuthSource: "oauth2"}, want: false},
		{name: "saved web authorization", cfg: &config.Config{AuthHeaderVal: "Bearer token", AuthSource: "garmin-web-session"}, want: true},
		{name: "saved web cookie", cfg: &config.Config{Headers: map[string]string{"Cookie": "SESSIONID=abc"}, AuthSource: "garmin-web-session"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := useGarminBrowserMutationSession(tt.cfg); got != tt.want {
				t.Fatalf("useGarminBrowserMutationSession() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGarminSavedSessionCookieParams(t *testing.T) {
	session := garminsession.Session{Cookies: []garminsession.Cookie{
		{Name: "session", Value: "abc", Domain: ".connect.garmin.com", Path: "/", Secure: true, HTTPOnly: true},
		{Name: "GARMIN-SSO", Value: "def", Domain: ".sso.garmin.com", Path: "/", Secure: true},
		{Name: "foreign", Value: "bad", Domain: ".example.com", Path: "/"},
	}}
	params := garminSavedSessionCookieParams(session)
	if len(params) != 2 {
		t.Fatalf("cookie params = %d, want 2", len(params))
	}
	if params[0].Domain != ".connect.garmin.com" || params[1].Domain != ".sso.garmin.com" {
		t.Fatalf("cookie domains = %q, %q", params[0].Domain, params[1].Domain)
	}
}

func TestGarminConnectLocationRejectsSSORedirect(t *testing.T) {
	if !isGarminConnectLocation("https://connect.garmin.com/app/workouts") {
		t.Fatal("connect app location rejected")
	}
	if isGarminConnectLocation("https://sso.garmin.com/portal/sso/en-US/sign-in") {
		t.Fatal("SSO redirect accepted as an authenticated Connect page")
	}
}

// TestNovelWorkoutsApplyHelpWires smoke-tests that the workouts apply command
// resolves at runtime and renders --help without error. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Always runs — do not delete this test when filling in real cases.
func TestNovelWorkoutsApplyHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"workouts", "apply", "--help"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workouts apply --help error = %v (novel command not wired correctly?)", err)
	}
}

func TestNovelWorkoutsApplyBehavior(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	draft := mustSaveBatchDraft(t, store, "Workout A", "2026-08-29")
	actualWorkout := make(map[string]any, len(draft.GarminPayload)+1)
	for key, value := range draft.GarminPayload {
		actualWorkout[key] = value
	}
	actualWorkout["workoutId"] = 42
	actualBody, err := json.Marshal(actualWorkout)
	if err != nil {
		t.Fatal(err)
	}

	workoutExists := false
	scheduleExists := false
	var posts []string
	session := testGarminMutationSession(func(_ context.Context, base, method, path string, _ any) (browserPostResponse, error) {
		if method == "POST" {
			posts = append(posts, base+path)
			switch {
			case path == "/workout-service/workout" && base == "a":
				workoutExists = true // The 427 response was ambiguous, but the write landed.
				return browserPostResponse{BaseURL: base, Status: 427, Body: `{"error":{"status-code":"427"}}`}, nil
			case path == "/workout-service/schedule/42":
				scheduleExists = true
				return browserPostResponse{BaseURL: base, Status: 200, Body: `{}`}, nil
			default:
				return browserPostResponse{}, fmt.Errorf("unexpected POST %s%s", base, path)
			}
		}
		switch {
		case path == garminBrowserMutationProbePath:
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		case path == "/workout-service/workouts?start=0&limit=100":
			if workoutExists {
				return browserPostResponse{BaseURL: base, Status: 200, Body: `[{"workoutId":42,"workoutName":"Workout A"}]`}, nil
			}
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		case path == "/workout-service/workout/42":
			return browserPostResponse{BaseURL: base, Status: 200, Body: string(actualBody)}, nil
		case path == "/calendar-service/year/2026/month/7":
			if scheduleExists {
				return browserPostResponse{BaseURL: base, Status: 200, Body: `{"calendarItems":[{"id":99,"itemType":"workout","title":"Workout A","date":"2026-08-29","workoutId":42}]}`}, nil
			}
			return browserPostResponse{BaseURL: base, Status: 200, Body: `{"calendarItems":[]}`}, nil
		default:
			return browserPostResponse{}, fmt.Errorf("unexpected GET %s%s", base, path)
		}
	})
	session.base = "a"

	result, err := applyGarminWorkoutDraftWithMutationSession(
		context.Background(),
		store,
		draft,
		draft.Date,
		"",
		session,
		0,
	)
	if err != nil {
		t.Fatalf("applyGarminWorkoutDraftWithMutationSession() error = %v", err)
	}
	if len(posts) != 2 || posts[0] != "a/workout-service/workout" || posts[1] != "b/workout-service/schedule/42" {
		t.Fatalf("POSTs = %#v, want one recovered upload and one schedule in the same session", posts)
	}
	if result["workout_id"] != "42" || result["scheduled_workout_id"] != "99" {
		t.Fatalf("result = %#v", result)
	}
	saved, err := store.Get(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.UploadedWorkout != "42" || saved.ScheduledID != "99" || saved.ScheduledDate != draft.Date {
		t.Fatalf("saved draft = %#v", saved)
	}
}
