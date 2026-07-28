package workoutdraft

import (
	"math"
	"path/filepath"
	"testing"
)

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

func TestPlanParsesNestedSetWorkout(t *testing.T) {
	draft, err := Plan("2mi easy warmup, 2 sets of (4 min at 10K effort, 60 sec float, 3 min at 5K effort, 60 sec float, 2 min at 3K effort) with 3 min jog between sets, 15 min cooldown", "2026-07-21", "Tue 7/21: 4/3/2 fartlek")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(draft.Workout.Steps) != 3 {
		t.Fatalf("top-level steps = %d, want warmup + set repeat + cooldown", len(draft.Workout.Steps))
	}
	set := draft.Workout.Steps[1]
	if set.StepType != "repeat" || set.Repeat != 2 || !set.SkipLastRecovery {
		t.Fatalf("set step = %#v, want two repeats that skip the final set recovery", set)
	}
	if len(set.Steps) != 6 {
		t.Fatalf("set children = %d, want 4min/float/3min/float/2min/set jog", len(set.Steps))
	}
	if set.Steps[1].StepType != "recovery" || set.Steps[1].DurationSec != 60 {
		t.Fatalf("first float = %#v, want 60-second recovery", set.Steps[1])
	}
	if set.Steps[5].StepType != "recovery" || set.Steps[5].DurationSec != 180 {
		t.Fatalf("set jog = %#v, want 3-minute recovery", set.Steps[5])
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

func TestPlanUsesLapButtonForUntimedFullRecovery(t *testing.T) {
	for _, prompt := range []string{
		"4x200m relaxed fast with full recovery",
		"4x200m relaxed fast full recovery",
		"6x10s hill sprint full recovery",
	} {
		draft, err := Plan(prompt, "2026-07-14", "")
		if err != nil {
			t.Fatalf("Plan(%q) returned error: %v", prompt, err)
		}
		repeat := draft.Workout.Steps[0]
		if repeat.StepType != "repeat" || len(repeat.Steps) != 2 {
			t.Fatalf("Plan(%q) should create an interval/recovery repeat, got %#v", prompt, repeat)
		}
		if repeat.Steps[0].StepType != "interval" {
			t.Fatalf("Plan(%q) should preserve the work rep as an interval, got %#v", prompt, repeat.Steps[0])
		}
		if repeat.Steps[1].EndCondition != "lap.button" || !repeat.SkipLastRecovery {
			t.Fatalf("Plan(%q) should use manual recovery and skip the final rest, got %#v", prompt, repeat)
		}
		segments := draft.GarminPayload["workoutSegments"].([]any)
		group := segments[0].(map[string]any)["workoutSteps"].([]any)[0].(map[string]any)
		if group["skipLastRestStep"] != true {
			t.Fatalf("Plan(%q) should skip the final Garmin recovery, got %#v", prompt, group)
		}
		recovery := group["workoutSteps"].([]any)[1].(map[string]any)
		if recovery["endCondition"].(map[string]any)["conditionTypeKey"] != "lap.button" {
			t.Fatalf("Plan(%q) should use Garmin lap-button recovery, got %#v", prompt, recovery)
		}
		if _, ok := recovery["endConditionValue"]; ok {
			t.Fatalf("Plan(%q) should not set a recovery duration, got %#v", prompt, recovery)
		}
	}
}

func TestPlanKeepsExplicitFullRecoveryDuration(t *testing.T) {
	draft, err := Plan("6x10s hill sprint, 2 min full recovery", "2026-07-14", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	repeat := draft.Workout.Steps[0]
	if repeat.SkipLastRecovery || repeat.Steps[1].DurationSec != 120 || repeat.Steps[1].EndCondition != "" {
		t.Fatalf("explicit two-minute recovery should stay timed, got %#v", repeat)
	}
}

func TestPlanUsesLapButtonForExplicitHillWalkDownRecovery(t *testing.T) {
	draft, err := Plan("30 min easy, 6x10s hill sprints with walk down the hill", "2026-07-09", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	segments := draft.GarminPayload["workoutSegments"].([]any)
	steps := segments[0].(map[string]any)["workoutSteps"].([]any)
	repeat := steps[1].(map[string]any)
	children := repeat["workoutSteps"].([]any)
	recovery := children[1].(map[string]any)
	condition := recovery["endCondition"].(map[string]any)
	if condition["conditionTypeKey"] != "lap.button" {
		t.Fatalf("walk-down recovery should wait for the lap button, got %#v", condition)
	}
	if _, ok := recovery["endConditionValue"]; ok {
		t.Fatalf("walk-down recovery should not have a timed end condition: %#v", recovery)
	}
	if repeat["skipLastRestStep"] != true {
		t.Fatalf("walk-down repeats should skip the final recovery, got %#v", repeat)
	}
	if draft.Workout.Duration != 0 {
		t.Fatalf("a lap-button recovery should leave total duration unknown, got %d", draft.Workout.Duration)
	}
	if draft.GarminPayload["estimatedDurationInSecs"] != nil {
		t.Fatalf("a lap-button recovery should not send an estimated duration: %#v", draft.GarminPayload)
	}
}

func TestPlanUsesLapButtonWhenWalkDownIsAttachedToHillSprints(t *testing.T) {
	draft, err := Plan("6x10s hill sprints walk down the hill", "2026-07-09", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	segments := draft.GarminPayload["workoutSegments"].([]any)
	steps := segments[0].(map[string]any)["workoutSteps"].([]any)
	repeat := steps[0].(map[string]any)
	children := repeat["workoutSteps"].([]any)
	condition := children[1].(map[string]any)["endCondition"].(map[string]any)
	if condition["conditionTypeKey"] != "lap.button" {
		t.Fatalf("attached walk-down recovery should wait for the lap button, got %#v", condition)
	}
}

func TestPlanUsesLapButtonForLongFormHillSprintDuration(t *testing.T) {
	draft, err := Plan("6x10 sec hill sprints walk down the hill", "2026-07-09", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	segments := draft.GarminPayload["workoutSegments"].([]any)
	steps := segments[0].(map[string]any)["workoutSteps"].([]any)
	children := steps[0].(map[string]any)["workoutSteps"].([]any)
	condition := children[1].(map[string]any)["endCondition"].(map[string]any)
	if condition["conditionTypeKey"] != "lap.button" {
		t.Fatalf("walk-down recovery should wait for the lap button, got %#v", condition)
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

func TestPlanUsesExplicitPaceRange(t *testing.T) {
	draft, err := Plan("1 km at 5:55-6:35/mi", "2026-07-28", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	segments := draft.GarminPayload["workoutSegments"].([]any)
	step := segments[0].(map[string]any)["workoutSteps"].([]any)[0].(map[string]any)
	target := step["targetType"].(map[string]any)
	if target["workoutTargetTypeKey"] != "pace.zone" {
		t.Fatalf("expected pace zone target, got %#v", target)
	}
	wantFast := 1000 / ((5*60 + 55) / 1.609344)
	wantSlow := 1000 / ((6*60 + 35) / 1.609344)
	if got := step["targetValueOne"].(float64); math.Abs(got-wantFast) > 0.001 {
		t.Fatalf("fast range bound = %v, want %v", got, wantFast)
	}
	if got := step["targetValueTwo"].(float64); math.Abs(got-wantSlow) > 0.001 {
		t.Fatalf("slow range bound = %v, want %v", got, wantSlow)
	}
}

func TestPlanPreservesDistancePickerUnit(t *testing.T) {
	for _, tc := range []struct {
		prompt  string
		meters  float64
		unitKey string
		factor  float64
	}{
		{prompt: "8.45 mi easy", meters: 8.45 * 1609.344, unitKey: "mile", factor: 160934.4},
		{prompt: "2.41 km easy", meters: 2410, unitKey: "kilometer", factor: 100000},
		{prompt: "800 m easy", meters: 800, unitKey: "meter", factor: 100},
	} {
		draft, err := Plan(tc.prompt, "2026-07-23", "")
		if err != nil {
			t.Fatalf("Plan(%q) returned error: %v", tc.prompt, err)
		}
		segments := draft.GarminPayload["workoutSegments"].([]any)
		step := segments[0].(map[string]any)["workoutSteps"].([]any)[0].(map[string]any)
		if got := step["endConditionValue"].(float64); math.Abs(got-tc.meters) > 0.001 {
			t.Fatalf("Plan(%q) end condition value = %#v, want %v meters", tc.prompt, got, tc.meters)
		}
		unit := step["preferredEndConditionUnit"].(map[string]any)
		if unit["unitKey"] != tc.unitKey {
			t.Fatalf("Plan(%q) picker unit = %#v, want %q", tc.prompt, unit, tc.unitKey)
		}
		if unit["factor"] != tc.factor {
			t.Fatalf("Plan(%q) picker factor = %#v, want %v", tc.prompt, unit["factor"], tc.factor)
		}
	}
}

func TestPlanUsesWarmupAndCooldownTypesForDistanceSteps(t *testing.T) {
	draft, err := Plan("2mi warmup easy, 3mi cooldown easy", "2026-07-28", "")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	segments := draft.GarminPayload["workoutSegments"].([]any)
	steps := segments[0].(map[string]any)["workoutSteps"].([]any)
	if got := steps[0].(map[string]any)["stepType"].(map[string]any)["stepTypeKey"]; got != "warmup" {
		t.Fatalf("warmup step type = %#v, want warmup", got)
	}
	if got := steps[1].(map[string]any)["stepType"].(map[string]any)["stepTypeKey"]; got != "cooldown" {
		t.Fatalf("cooldown step type = %#v, want cooldown", got)
	}
}

func TestStoreSavePreservesAppliedGarminLinkageWhenReplanningSameDraft(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	draft, err := Plan("2mi warmup easy", "2026-07-28", "Tuesday: Warmup")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if err := store.Save(draft); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if err := store.MarkApplied(draft.ID, "workout-42", "schedule-99", draft.Date); err != nil {
		t.Fatalf("MarkApplied returned error: %v", err)
	}

	replanned, err := Plan(draft.Prompt, draft.Date, draft.Name)
	if err != nil {
		t.Fatalf("replanning returned error: %v", err)
	}
	if err := store.Save(replanned); err != nil {
		t.Fatalf("saving replanned draft returned error: %v", err)
	}
	saved, err := store.Get(draft.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if saved.UploadedWorkout != "workout-42" || saved.ScheduledID != "schedule-99" || saved.ScheduledDate != draft.Date || saved.AppliedAt == nil {
		t.Fatalf("replanned draft lost Garmin linkage: %#v", saved)
	}
}

func TestPlanRejectsInvalidDate(t *testing.T) {
	if _, err := Plan("10 min warmup", "07/01/2026", ""); err == nil {
		t.Fatal("expected invalid date error")
	}
}
