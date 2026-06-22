package workoutdraft

import "testing"

func TestPlanParsesRepeatWorkout(t *testing.T) {
	draft, err := Plan("10 min warmup, 6x800m at 5K pace with 2 min jog, 10 min cooldown", "2026-07-01", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if draft.ID == "" {
		t.Fatal("expected draft id")
	}
	if draft.Workout.Name != "July 1: Run 6X800M" {
		t.Fatalf("unexpected workout name: %s", draft.Workout.Name)
	}
	if len(draft.Workout.Steps) != 3 {
		t.Fatalf("expected 3 top-level steps, got %d", len(draft.Workout.Steps))
	}
	repeat := draft.Workout.Steps[1]
	if repeat.StepType != "repeat" || repeat.Repeat != 6 {
		t.Fatalf("expected 6-step repeat, got %#v", repeat)
	}
	if len(repeat.Steps) != 2 {
		t.Fatalf("expected interval + recovery child steps, got %d", len(repeat.Steps))
	}
	if draft.GarminPayload["workoutName"] != "July 1: Run 6X800M" {
		t.Fatalf("payload did not carry workout name: %#v", draft.GarminPayload["workoutName"])
	}
}

func TestPlanParsesScreenshotStyleWorkoutWithNotesAndDefaultStrideRecovery(t *testing.T) {
	draft, err := Plan("35min E + Drills + 4x20s strides 全程放松，不追配速", "2026-06-23", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if draft.Workout.Name != "June 23: 35E + Drills + 4x20s strides" {
		t.Fatalf("unexpected workout name: %s", draft.Workout.Name)
	}
	if len(draft.Workout.Notes) != 1 || draft.Workout.Notes[0] != "Drills" {
		t.Fatalf("expected drills to be retained as a note, got %#v", draft.Workout.Notes)
	}
	repeat := draft.Workout.Steps[1]
	if repeat.StepType != "repeat" || repeat.Repeat != 4 {
		t.Fatalf("expected 4-stride repeat, got %#v", repeat)
	}
	if len(repeat.Steps) != 2 || repeat.Steps[1].DurationSec != 60 {
		t.Fatalf("expected default 60 second stride recovery, got %#v", repeat.Steps)
	}
}

func TestPlanParsesHillSprintsWithFullRecovery(t *testing.T) {
	draft, err := Plan("30min E + 6x10\" Hill Sprint 每次全恢复", "2026-06-25", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if draft.Workout.Name != "June 25: 30E + 6x10s hill sprints" {
		t.Fatalf("unexpected workout name: %s", draft.Workout.Name)
	}
	repeat := draft.Workout.Steps[1]
	if repeat.StepType != "repeat" || repeat.Repeat != 6 {
		t.Fatalf("expected 6 hill sprint repeats, got %#v", repeat)
	}
	if len(repeat.Steps) != 2 || repeat.Steps[1].DurationSec != 90 {
		t.Fatalf("expected default 90 second full recovery, got %#v", repeat.Steps)
	}
}

func TestPlanAddsPaceZoneForExplicitPace(t *testing.T) {
	draft, err := Plan("10 min warmup, 4x1km at 4:30/km with 90 sec jog, 10 min cooldown", "2026-07-08", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	segments := draft.GarminPayload["workoutSegments"].([]any)
	steps := segments[0].(map[string]any)["workoutSteps"].([]any)
	repeat := steps[1].(map[string]any)
	children := repeat["workoutSteps"].([]any)
	interval := children[0].(map[string]any)
	target := interval["targetType"].(map[string]any)
	if target["workoutTargetTypeKey"] != "pace.zone" {
		t.Fatalf("expected pace zone target, got %#v", target)
	}
	if interval["targetValueOne"].(float64) <= interval["targetValueTwo"].(float64) {
		t.Fatalf("Garmin pace target should store faster speed first: %#v", interval)
	}
}

func TestPlanRejectsInvalidDate(t *testing.T) {
	if _, err := Plan("10 min warmup", "07/01/2026", ""); err == nil {
		t.Fatal("expected invalid date error")
	}
}
