// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"testing"

	"garmin-connect-workout-cli/internal/config"
)

func TestGarminWorkoutsListRequestUsesCollectionEndpoint(t *testing.T) {
	path, params := garminWorkoutsListRequest(20)
	if path != "/workout-service/workouts" {
		t.Fatalf("path = %q, want plural workouts collection endpoint", path)
	}
	if params["start"] != "0" || params["limit"] != "20" {
		t.Fatalf("params = %#v, want start=0 and limit=20", params)
	}
}

func TestGarminWorkoutWriteTarget(t *testing.T) {
	tests := []struct {
		name      string
		replaceID string
		method    string
		path      string
	}{
		{name: "create", method: "POST", path: "/workout-service/workout"},
		{name: "replace", replaceID: "1631264541", method: "PUT", path: "/workout-service/workout/1631264541"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, path := garminWorkoutWriteTarget(tt.replaceID)
			if method != tt.method || path != tt.path {
				t.Fatalf("target = %s %s, want %s %s", method, path, tt.method, tt.path)
			}
		})
	}
}

func TestReplacementDoesNotCreateAnotherScheduleByDefault(t *testing.T) {
	if got := resolveWorkoutSchedule("", false, "2026-07-16", "1631264541"); got != "" {
		t.Fatalf("replacement schedule = %q, want empty", got)
	}
	if got := resolveWorkoutSchedule("2026-07-16", false, "2026-07-16", "1631264541"); got != "2026-07-16" {
		t.Fatalf("explicit replacement schedule = %q, want requested date", got)
	}
	if got := resolveWorkoutSchedule("", false, "2026-07-16", ""); got != "2026-07-16" {
		t.Fatalf("new workout schedule = %q, want draft date", got)
	}
}

func TestGarminWorkoutReadUsesBrowserForCookieOnlySession(t *testing.T) {
	if !useGarminBrowserRead(&config.Config{Headers: map[string]string{"Cookie": "SESSIONID=abc"}}) {
		t.Fatal("cookie-only session should use the saved browser profile")
	}
	if useGarminBrowserRead(&config.Config{AuthHeaderVal: "Bearer token"}) {
		t.Fatal("bearer session should use the direct API client")
	}
}

func TestGarminBrowserReadPathIncludesQuery(t *testing.T) {
	got := garminBrowserReadPath("/workout-service/workouts", map[string]string{"limit": "20", "start": "0"})
	if got != "/workout-service/workouts?limit=20&start=0" {
		t.Fatalf("browser read path = %q", got)
	}
}
