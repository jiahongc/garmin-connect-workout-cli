// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"math"
)

func compareGarminWorkoutPayload(expected map[string]any, actualBody string) (bool, string, error) {
	contract, err := garminWorkoutContract(expected)
	if err != nil {
		return false, "", fmt.Errorf("building expected Garmin workout contract: %w", err)
	}
	var actual any
	if err := json.Unmarshal([]byte(actualBody), &actual); err != nil {
		return false, "", fmt.Errorf("decoding live Garmin workout: %w", err)
	}
	if wrapped, ok := actual.(map[string]any); ok {
		if data, exists := wrapped["data"]; exists {
			actual = data
		}
	}
	if mismatch := compareGarminContractValue(contract, actual, "workout"); mismatch != "" {
		return false, mismatch, nil
	}
	return true, "", nil
}

func garminWorkoutContract(payload map[string]any) (map[string]any, error) {
	segments, ok := payload["workoutSegments"].([]any)
	if !ok {
		return nil, fmt.Errorf("workoutSegments is not an array")
	}
	projectedSegments := make([]any, 0, len(segments))
	for _, rawSegment := range segments {
		segment, ok := rawSegment.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("workout segment is not an object")
		}
		rawSteps, ok := segment["workoutSteps"].([]any)
		if !ok {
			return nil, fmt.Errorf("workoutSteps is not an array")
		}
		steps, err := garminStepContracts(rawSteps)
		if err != nil {
			return nil, err
		}
		projectedSegments = append(projectedSegments, map[string]any{"workoutSteps": steps})
	}
	return map[string]any{
		"workoutName":     payload["workoutName"],
		"workoutSegments": projectedSegments,
	}, nil
}

func garminStepContracts(rawSteps []any) ([]any, error) {
	steps := make([]any, 0, len(rawSteps))
	for _, rawStep := range rawSteps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("workout step is not an object")
		}
		contract := map[string]any{}
		copyGarminContractField(contract, step, "type")
		copyGarminContractField(contract, step, "description")
		copyGarminNestedContract(contract, step, "stepType", "stepTypeKey")
		copyGarminNestedContract(contract, step, "endCondition", "conditionTypeKey")
		copyGarminContractField(contract, step, "endConditionValue")
		copyGarminNestedContract(contract, step, "preferredEndConditionUnit", "unitKey", "factor")
		copyGarminNestedContract(contract, step, "targetType", "workoutTargetTypeKey")
		copyGarminContractField(contract, step, "targetValueOne")
		copyGarminContractField(contract, step, "targetValueTwo")
		copyGarminContractField(contract, step, "numberOfIterations")
		copyGarminContractField(contract, step, "skipLastRestStep")
		if rawChildren, exists := step["workoutSteps"]; exists {
			children, ok := rawChildren.([]any)
			if !ok {
				return nil, fmt.Errorf("nested workoutSteps is not an array")
			}
			projected, err := garminStepContracts(children)
			if err != nil {
				return nil, err
			}
			contract["workoutSteps"] = projected
		}
		steps = append(steps, contract)
	}
	return steps, nil
}

func copyGarminContractField(dst, src map[string]any, key string) {
	if value, exists := src[key]; exists {
		dst[key] = value
	}
}

func copyGarminNestedContract(dst, src map[string]any, key string, fields ...string) {
	raw, exists := src[key]
	if !exists {
		return
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return
	}
	projected := map[string]any{}
	for _, field := range fields {
		copyGarminContractField(projected, object, field)
	}
	dst[key] = projected
}

func compareGarminContractValue(expected, actual any, path string) string {
	switch expectedValue := expected.(type) {
	case map[string]any:
		actualValue, ok := actual.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s is %T, want object", path, actual)
		}
		for key, value := range expectedValue {
			actualChild, exists := actualValue[key]
			if !exists {
				return fmt.Sprintf("%s.%s is missing", path, key)
			}
			if mismatch := compareGarminContractValue(value, actualChild, path+"."+key); mismatch != "" {
				return mismatch
			}
		}
		return ""
	case []any:
		actualValue, ok := actual.([]any)
		if !ok {
			return fmt.Sprintf("%s is %T, want array", path, actual)
		}
		if len(actualValue) != len(expectedValue) {
			return fmt.Sprintf("%s has %d items, want %d", path, len(actualValue), len(expectedValue))
		}
		for index := range expectedValue {
			if mismatch := compareGarminContractValue(expectedValue[index], actualValue[index], fmt.Sprintf("%s[%d]", path, index)); mismatch != "" {
				return mismatch
			}
		}
		return ""
	case float64:
		actualValue, ok := actual.(float64)
		if !ok {
			return fmt.Sprintf("%s is %T, want number", path, actual)
		}
		tolerance := 0.000001 * math.Max(1, math.Abs(expectedValue))
		if math.Abs(expectedValue-actualValue) > tolerance {
			return fmt.Sprintf("%s is %v, want %v", path, actualValue, expectedValue)
		}
		return ""
	default:
		if fmt.Sprint(expected) != fmt.Sprint(actual) {
			return fmt.Sprintf("%s is %v, want %v", path, actual, expected)
		}
		return ""
	}
}
