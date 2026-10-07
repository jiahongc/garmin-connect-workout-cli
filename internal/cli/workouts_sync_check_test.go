// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"io"
	"testing"

	"garmin-connect-workout-cli/internal/workoutdraft"
)

// TestNovelWorkoutsSyncCheckHelpWires smoke-tests that the workouts sync-check command
// resolves at runtime and renders --help without error. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Always runs — do not delete this test when filling in real cases.
func TestNovelWorkoutsSyncCheckHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"workouts", "sync-check", "--help"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workouts sync-check --help error = %v (novel command not wired correctly?)", err)
	}
}

func TestDraftScheduledOnRequiresScheduleEntry(t *testing.T) {
	cases := []struct {
		name  string
		draft workoutdraft.Draft
		date  string
		want  bool
	}{
		{"not uploaded", workoutdraft.Draft{Date: "2026-10-07"}, "2026-10-07", false},
		{"uploaded with --no-schedule", workoutdraft.Draft{Date: "2026-10-07", UploadedWorkout: "1"}, "2026-10-07", false},
		{"scheduled on date", workoutdraft.Draft{UploadedWorkout: "1", ScheduledID: "2", ScheduledDate: "2026-10-07"}, "2026-10-07", true},
		{"scheduled on other date", workoutdraft.Draft{Date: "2026-10-07", UploadedWorkout: "1", ScheduledID: "2", ScheduledDate: "2026-10-08"}, "2026-10-07", false},
		{"scheduled, no date filter", workoutdraft.Draft{UploadedWorkout: "1", ScheduledID: "2", ScheduledDate: "2026-10-08"}, "", true},
	}
	for _, tc := range cases {
		if got := draftScheduledOn(tc.draft, tc.date); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
