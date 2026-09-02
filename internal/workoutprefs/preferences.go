// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package workoutprefs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"garmin-connect-workout-cli/internal/cliutil"
)

const (
	CurrentVersion       = 1
	Ask                  = "ask"
	TripleStrideDuration = "3x stride duration"
	fileName             = "workout-preferences.json"
)

// Preferences contains only choices the user explicitly elected to save.
// The value "ask" means the planner must request the detail each time.
type Preferences struct {
	Version            int    `json:"version"`
	StrideRecovery     string `json:"stride_recovery"`
	HillSprintRecovery string `json:"hill_sprint_recovery"`
}

func Default() Preferences {
	return Preferences{
		Version:            CurrentVersion,
		StrideRecovery:     Ask,
		HillSprintRecovery: Ask,
	}
}

func Path() (string, error) {
	dir, err := cliutil.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

func Load() (Preferences, string, bool, error) {
	path, err := Path()
	if err != nil {
		return Preferences{}, "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), path, false, nil
		}
		return Preferences{}, path, false, fmt.Errorf("reading workout preferences: %w", err)
	}
	var prefs Preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return Preferences{}, path, false, fmt.Errorf("parsing workout preferences: %w", err)
	}
	if prefs.Version > CurrentVersion {
		return Preferences{}, path, false, fmt.Errorf("workout preferences use unsupported version %d", prefs.Version)
	}
	prefs = normalize(prefs)
	return prefs, path, true, nil
}

func Save(prefs Preferences) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	prefs = normalize(prefs)
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding workout preferences: %w", err)
	}
	if err := cliutil.AtomicWritePrivateFile(path, append(data, '\n'), 0o600, 0o700); err != nil {
		return "", fmt.Errorf("saving workout preferences: %w", err)
	}
	return path, nil
}

func normalize(prefs Preferences) Preferences {
	prefs.Version = CurrentVersion
	prefs.StrideRecovery = normalizeRecovery(prefs.StrideRecovery)
	prefs.HillSprintRecovery = normalizeRecovery(prefs.HillSprintRecovery)
	return prefs
}

func normalizeRecovery(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, Ask) {
		return Ask
	}
	return value
}
