// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"testing"

	"garmin-connect-workout-cli/internal/workoutdraft"
)

func TestCompareGarminWorkoutPayloadIgnoresServerFieldsAndAllowsFloatRounding(t *testing.T) {
	draft, err := workoutdraft.Plan("2mi warmup easy", "2026-07-28", "Tuesday: Warmup")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	data, err := json.Marshal(draft.GarminPayload)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var live map[string]any
	if err := json.Unmarshal(data, &live); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	live["workoutId"] = float64(42)
	steps := live["workoutSegments"].([]any)[0].(map[string]any)["workoutSteps"].([]any)
	steps[0].(map[string]any)["endConditionValue"] = 3218.6880001
	steps[0].(map[string]any)["stepId"] = float64(999)

	body, _ := json.Marshal(live)
	matches, mismatch, err := compareGarminWorkoutPayload(draft.GarminPayload, string(body))
	if err != nil {
		t.Fatalf("compareGarminWorkoutPayload returned error: %v", err)
	}
	if !matches {
		t.Fatalf("payload mismatch = %q", mismatch)
	}
}

func TestCompareGarminWorkoutPayloadDetectsDistanceAndStepTypeMismatch(t *testing.T) {
	draft, err := workoutdraft.Plan("2mi warmup easy", "2026-07-28", "Tuesday: Warmup")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	data, _ := json.Marshal(draft.GarminPayload)
	var live map[string]any
	_ = json.Unmarshal(data, &live)
	step := live["workoutSegments"].([]any)[0].(map[string]any)["workoutSteps"].([]any)[0].(map[string]any)
	step["endConditionValue"] = float64(2)
	step["stepType"].(map[string]any)["stepTypeKey"] = "interval"
	body, _ := json.Marshal(live)

	matches, mismatch, err := compareGarminWorkoutPayload(draft.GarminPayload, string(body))
	if err != nil {
		t.Fatalf("compareGarminWorkoutPayload returned error: %v", err)
	}
	if matches || mismatch == "" {
		t.Fatalf("matches = %v, mismatch = %q", matches, mismatch)
	}
}
