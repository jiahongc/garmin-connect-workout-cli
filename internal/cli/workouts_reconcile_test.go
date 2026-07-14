// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"
	"testing"

	"garmin-connect-workout-cli/internal/types"
)

func TestBuildWorkoutReconcilePlan(t *testing.T) {
	workouts := []types.Workout{
		{WorkoutId: "1", WorkoutName: "old"},
		{WorkoutId: "2", WorkoutName: "future-a"},
		{WorkoutId: "3", WorkoutName: "future-b"},
	}
	schedules := []workoutReconcileSchedule{
		{Name: "future-a", Date: "2026-07-16"},
	}

	plan, err := buildWorkoutReconcilePlan(workouts, []string{"future-a", "future-b"}, schedules, 1)
	if err != nil {
		t.Fatalf("buildWorkoutReconcilePlan() error = %v", err)
	}
	if len(plan.Keep) != 2 || len(plan.Delete) != 1 || len(plan.Schedule) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Delete[0].WorkoutId != "1" || plan.Schedule[0].Workout.WorkoutId != "2" {
		t.Fatalf("plan targets = %#v", plan)
	}
}

func TestBuildWorkoutReconcilePlanRejectsMissingKeep(t *testing.T) {
	_, err := buildWorkoutReconcilePlan(
		[]types.Workout{{WorkoutId: "1", WorkoutName: "old"}},
		[]string{"missing"},
		nil,
		1,
	)
	if err == nil {
		t.Fatal("buildWorkoutReconcilePlan() error = nil, want missing-keep error")
	}
}

func TestBuildWorkoutReconcilePlanAllowsUnguardedDryRun(t *testing.T) {
	plan, err := buildWorkoutReconcilePlan(
		[]types.Workout{
			{WorkoutId: "1", WorkoutName: "old"},
			{WorkoutId: "2", WorkoutName: "keep"},
		},
		[]string{"keep"},
		nil,
		-1,
	)
	if err != nil {
		t.Fatalf("buildWorkoutReconcilePlan() error = %v", err)
	}
	if len(plan.Delete) != 1 {
		t.Fatalf("len(plan.Delete) = %d, want 1", len(plan.Delete))
	}
}

func TestParseWorkoutReconcileScheduleSplitsAtDate(t *testing.T) {
	got, err := parseWorkoutReconcileSchedules([]string{"A=B=2026-07-16"})
	if err != nil {
		t.Fatalf("parseWorkoutReconcileSchedules() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "A=B" || got[0].Date != "2026-07-16" {
		t.Fatalf("parseWorkoutReconcileSchedules() = %#v", got)
	}
}

func TestValidateGarminWorkoutListLimit(t *testing.T) {
	if err := validateGarminWorkoutListLimit(99, 100); err != nil {
		t.Fatalf("validateGarminWorkoutListLimit(99, 100) error = %v", err)
	}
	if err := validateGarminWorkoutListLimit(100, 100); err == nil {
		t.Fatal("validateGarminWorkoutListLimit(100, 100) error = nil, want incomplete-list error")
	}
}

func TestDecodeGarminWorkoutPage(t *testing.T) {
	for _, body := range []string{
		`[{"workoutId":"1","workoutName":"A"}]`,
		`{"data":[{"workoutId":"1","workoutName":"A"}]}`,
		`[{"workoutId":1620262624,"workoutName":"A"}]`,
	} {
		got, err := decodeGarminWorkoutPage([]byte(body))
		if err != nil {
			t.Fatalf("decodeGarminWorkoutPage(%s) error = %v", body, err)
		}
		wantID := "1"
		if strings.Contains(body, "1620262624") {
			wantID = "1620262624"
		}
		if len(got) != 1 || got[0].WorkoutId != wantID || got[0].WorkoutName != "A" {
			t.Fatalf("decodeGarminWorkoutPage(%s) = %#v", body, got)
		}
	}
}

func TestFindGarminCalendarWorkoutInData(t *testing.T) {
	target := workoutReconcileTarget{
		Workout: types.Workout{WorkoutId: "1620262601", WorkoutName: "Thu 7/16: 10mi MLR + hills"},
		Date:    "2026-07-16",
	}
	body := []byte(`{"calendarItems":[{"id":1711853798,"itemType":"workout","title":"Thu 7/16: 10mi MLR + hills","date":"2026-07-16","workoutId":1620262601}]}`)

	id, found, err := findGarminCalendarWorkoutInData(body, target)
	if err != nil {
		t.Fatalf("findGarminCalendarWorkoutInData() error = %v", err)
	}
	if !found || id != "1711853798" {
		t.Fatalf("findGarminCalendarWorkoutInData() = (%q, %v), want (%q, true)", id, found, "1711853798")
	}
}
