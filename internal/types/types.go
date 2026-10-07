// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package types

import "encoding/json"

type Workout struct {
	WorkoutId               string          `json:"workoutId"`
	WorkoutName             string          `json:"workoutName"`
	Description             string          `json:"description"`
	SportType               json.RawMessage `json:"sportType"`
	EstimatedDurationInSecs int             `json:"estimatedDurationInSecs"`
	WorkoutSegments         json.RawMessage `json:"workoutSegments"`
}
