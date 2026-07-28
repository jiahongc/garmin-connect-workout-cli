// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"garmin-connect-workout-cli/internal/cliutil"
)

const (
	garminMutationCooldownFloor = 30 * time.Minute
	garminMutationCooldownMax   = 2 * time.Hour
)

type garminMutationCircuitState struct {
	Consecutive429 int       `json:"consecutive_429"`
	BlockedUntil   time.Time `json:"blocked_until"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func garminMutationCircuitPath() (string, error) {
	dir, err := cliutil.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "garmin-mutation-circuit.json"), nil
}

func checkGarminMutationCircuit(now time.Time) error {
	path, err := garminMutationCircuitPath()
	if err != nil {
		return configErr(err)
	}
	return checkGarminMutationCircuitAt(path, now)
}

func checkGarminMutationCircuitAt(path string, now time.Time) error {
	state, ok, err := loadGarminMutationCircuit(path)
	if err != nil {
		return configErr(err)
	}
	if !ok || !now.Before(state.BlockedUntil) {
		return nil
	}
	return rateLimitErr(fmt.Errorf(
		"Garmin mutation circuit is open until %s (%s remaining); no browser was opened and no request was sent",
		state.BlockedUntil.Format(time.RFC3339),
		state.BlockedUntil.Sub(now).Round(time.Second),
	))
}

func recordGarminMutationRateLimit(body string, now time.Time) error {
	path, err := garminMutationCircuitPath()
	if err != nil {
		return err
	}
	return recordGarminMutationRateLimitAt(path, body, now)
}

func recordGarminMutationRateLimitAt(path, body string, now time.Time) error {
	state, ok, err := loadGarminMutationCircuit(path)
	if err != nil {
		return err
	}
	if !ok || now.Sub(state.UpdatedAt) > garminMutationCooldownMax {
		state = garminMutationCircuitState{}
	}
	state.Consecutive429++
	delay := garminRateLimitDelay(body, state.Consecutive429)
	state.BlockedUntil = now.Add(delay)
	state.UpdatedAt = now

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return cliutil.AtomicWritePrivateFile(path, append(data, '\n'), 0o600, 0o700)
}

func clearGarminMutationCircuit() error {
	path, err := garminMutationCircuitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing Garmin mutation circuit: %w", err)
	}
	return nil
}

func loadGarminMutationCircuit(path string) (garminMutationCircuitState, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return garminMutationCircuitState{}, false, nil
		}
		return garminMutationCircuitState{}, false, fmt.Errorf("reading Garmin mutation circuit: %w", err)
	}
	var state garminMutationCircuitState
	if err := json.Unmarshal(data, &state); err != nil {
		return garminMutationCircuitState{}, false, fmt.Errorf("parsing Garmin mutation circuit: %w", err)
	}
	return state, true, nil
}

func garminRateLimitDelay(body string, consecutive int) time.Duration {
	delay := garminMutationCooldownFloor
	var parsed struct {
		RetryAfter int `json:"retry_after"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil {
		serverDelay := time.Duration(parsed.RetryAfter) * time.Second
		if serverDelay > delay {
			delay = serverDelay
		}
	}
	for attempt := 1; attempt < consecutive && delay < garminMutationCooldownMax; attempt++ {
		delay *= 2
	}
	if delay > garminMutationCooldownMax {
		return garminMutationCooldownMax
	}
	return delay
}
