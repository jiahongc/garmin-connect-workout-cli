// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package workoutdraft

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"garmin-connect-workout-cli/internal/workoutprefs"
)

// Clarification describes one material detail that must be resolved before the
// CLI can produce reliable Garmin steps.
type Clarification struct {
	Code     string `json:"code"`
	Question string `json:"question"`
}

// Clarifications returns only ambiguities that would change workout structure,
// duration, distance, recovery, or intensity. Common running shorthand remains
// automatic.
func Clarifications(prompt string) []Clarification {
	parts := splitPromptParts(prompt)
	var questions []Clarification
	seen := map[string]struct{}{}
	add := func(code, question string) {
		key := code + "\x00" + question
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		questions = append(questions, Clarification{Code: code, Question: question})
	}

	if distanceRange := statedDistanceRange(prompt); distanceRange != "" {
		add(
			"distance_range",
			fmt.Sprintf("For %q, what exact total distance should Garmin use, and do the strides and recoveries count inside that total?", distanceRange),
		)
	}

	if total := statedTotalDistance(prompt); total != "" && workoutHasMultipleComponents(prompt) {
		add(
			"total_distance",
			fmt.Sprintf("For %q, does that total include every warmup, repeat, recovery, and cooldown, and should I calculate the cooldown needed to reach it?", total),
		)
	}

	for index, raw := range parts {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)

		if missingStepMeasure(lower, "warmup", "warm up") {
			add("step_duration", "How long or how far should the warmup be?")
		}
		if missingStepMeasure(lower, "cooldown", "cool down", "cd") {
			add("step_duration", "How long or how far should the cooldown be?")
		}

		repeatLabel, missingUnit := ambiguousUnitlessRepeat(part)
		if missingUnit {
			add(
				"repeat_unit",
				fmt.Sprintf("For %q, is the repeat distance in meters, kilometers, or miles?", repeatLabel),
			)
			continue
		}

		repeat, ok := repeatForClarification(part)
		if !ok {
			continue
		}
		if strings.TrimSpace(repeat.Steps[0].Target) == "" {
			add(
				"repeat_target",
				fmt.Sprintf("What pace or effort should the %s work repeats use?", repeat.Steps[0].Name),
			)
		}
		if repeatHasRecovery(repeat) {
			continue
		}
		if index+1 < len(parts) && isExplicitRecovery(strings.TrimSpace(parts[index+1])) {
			continue
		}
		if hasVagueRecoveryCue(lower) {
			add(
				"repeat_recovery",
				fmt.Sprintf("For %s, how long or how far is the recovery, or should it be full recovery controlled by the Lap button?", repeat.Name),
			)
			continue
		}
		add(
			"repeat_recovery",
			fmt.Sprintf("What recovery should follow each %s work repeat (for example, 2 min jog, 400 m jog, or full recovery)?", repeat.Steps[0].Name),
		)
	}
	return questions
}

// ApplyRecoveryPreferences adds only recovery values the user explicitly
// saved. Explicit recovery in the workout text always wins.
func ApplyRecoveryPreferences(prompt, strideRecovery, hillRecovery string) string {
	parts := splitPromptParts(prompt)
	for index, raw := range parts {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		repeat, ok := repeatForClarification(part)
		if !ok || repeatHasRecovery(repeat) {
			parts[index] = part
			continue
		}
		if index+1 < len(parts) && isExplicitRecovery(strings.TrimSpace(parts[index+1])) {
			parts[index] = part
			continue
		}

		lower := strings.ToLower(part)
		preference := ""
		switch {
		case strings.Contains(lower, "stride"):
			preference = strideRecovery
		case strings.Contains(lower, "hill"):
			preference = hillRecovery
		}
		preference = strings.TrimSpace(preference)
		if preference == "" || strings.EqualFold(preference, "ask") {
			parts[index] = part
			continue
		}
		if preference == workoutprefs.TripleStrideDuration {
			if repeat.Steps[0].DurationSec == 0 {
				parts[index] = part
				continue
			}
			preference = fmt.Sprintf("%d sec recovery", repeat.Steps[0].DurationSec*3)
		}

		candidate := part + " with " + preference
		resolved, ok := repeatForClarification(candidate)
		if !ok || !repeatHasRecovery(resolved) {
			parts[index] = part
			continue
		}
		parts[index] = candidate
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return strings.Join(parts, ", ")
}

func repeatForClarification(part string) (Step, bool) {
	if repeat, ok := parseRepeat(part); ok {
		return repeat, true
	}
	return parseTimeRepeat(part)
}

func repeatHasRecovery(repeat Step) bool {
	for _, child := range repeat.Steps[1:] {
		if child.StepType == "recovery" {
			return true
		}
	}
	return false
}

func isExplicitRecovery(text string) bool {
	if !isRecoveryText(text) && manualRecoveryText(text) == "" {
		return false
	}
	if recovery, ok := parseRecovery(text); ok && recovery.StepType == "recovery" {
		return true
	}
	_, ok := parseManualRecovery(text)
	return ok
}

func hasVagueRecoveryCue(lower string) bool {
	index := strings.LastIndex(lower, " with ")
	if index < 0 {
		return false
	}
	recovery := strings.TrimSpace(lower[index+len(" with "):])
	return isRecoveryText(recovery)
}

func missingStepMeasure(lower string, labels ...string) bool {
	found := false
	for _, label := range labels {
		if strings.HasPrefix(lower, label) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	return !regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:min|mins|minute|minutes|sec|secs|second|seconds|km|k|m|mi|mile|miles)\b`).MatchString(lower)
}

func ambiguousUnitlessRepeat(text string) (string, bool) {
	match := regexp.MustCompile(`(?i)\b(\d+)\s*x\s*(\d+(?:\.\d+)?)`).FindStringSubmatchIndex(text)
	if match == nil {
		return "", false
	}
	repeatLabel := strings.TrimSpace(text[match[0]:match[1]])
	remaining := text[match[1]:]
	if regexp.MustCompile(`(?i)^\s*(?:km|k|m|mi|mile|miles|min|mins|minute|minutes|s|sec|secs|second|seconds|")\b?`).MatchString(remaining) {
		return "", false
	}
	distance, _ := strconv.ParseFloat(text[match[4]:match[5]], 64)
	if distance >= 100 {
		return "", false
	}
	return repeatLabel, true
}

func statedTotalDistance(prompt string) string {
	match := regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s*(km|k|mi|mile|miles)\s+total\b`).FindStringSubmatch(prompt)
	if match == nil {
		return ""
	}
	return strings.TrimSpace(match[0])
}

func statedDistanceRange(prompt string) string {
	match := regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\s*(?:-|–|to)\s*\d+(?:\.\d+)?\s*(?:km|k|mi|mile|miles)\b`).FindString(prompt)
	return strings.TrimSpace(match)
}

func workoutHasMultipleComponents(prompt string) bool {
	lower := strings.ToLower(prompt)
	return strings.ContainsAny(prompt, ",+;") ||
		strings.Contains(lower, "warm") ||
		strings.Contains(lower, "cool") ||
		regexp.MustCompile(`\d+\s*x\s*\d+`).MatchString(lower)
}
