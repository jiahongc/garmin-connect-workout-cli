// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package types

import "encoding/json"

type ScheduledWorkout struct {
	ScheduledWorkoutId string          `json:"scheduledWorkoutId"`
	WorkoutId          string          `json:"workoutId"`
	Date               string          `json:"date"`
	Workout            json.RawMessage `json:"workout"`
}

type Workout struct {
	WorkoutId               string          `json:"workoutId"`
	WorkoutName             string          `json:"workoutName"`
	Description             string          `json:"description"`
	SportType               json.RawMessage `json:"sportType"`
	EstimatedDurationInSecs int             `json:"estimatedDurationInSecs"`
	WorkoutSegments         json.RawMessage `json:"workoutSegments"`
}

type WorkoutTypes struct {
	SportTypes     json.RawMessage `json:"sportTypes"`
	StepTypes      json.RawMessage `json:"stepTypes"`
	ConditionTypes json.RawMessage `json:"conditionTypes"`
	TargetTypes    json.RawMessage `json:"targetTypes"`
}
