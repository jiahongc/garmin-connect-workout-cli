// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"garmin-connect-workout-cli/internal/garminsession"
	"garmin-connect-workout-cli/internal/workoutdraft"
)

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
	if !isGarminConnectAppLocation("https://connect.garmin.com/app/workouts") {
		t.Fatal("connect app location rejected")
	}
	if isGarminConnectAppLocation("https://sso.garmin.com/portal/sso/en-US/sign-in") {
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

func TestDeleteCommandsOnlyPreviewWithoutApply(t *testing.T) {
	for _, args := range [][]string{
		{"workouts", "delete", "1722206579", "--json"},
		{"schedule", "delete", "1801324592", "--json"},
		{"workouts", "delete", "1722206579", "--apply", "--dry-run", "--json"},
	} {
		cmd := RootCmd()
		var out bytes.Buffer
		cmd.SetArgs(args)
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var preview map[string]any
		if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
			t.Fatalf("%v: output %q: %v", args, out.String(), err)
		}
		if preview["dry_run"] != true || preview["method"] != "DELETE" {
			t.Fatalf("%v: expected a DELETE preview, got %v", args, preview)
		}
	}
}
