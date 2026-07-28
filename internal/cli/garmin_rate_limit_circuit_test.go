// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGarminRateLimitDelayUsesConservativeExponentialBackoff(t *testing.T) {
	if got := garminRateLimitDelay(`{"retry_after":30}`, 1); got != 30*time.Minute {
		t.Fatalf("first delay = %s, want 30m", got)
	}
	if got := garminRateLimitDelay(`{"retry_after":30}`, 2); got != time.Hour {
		t.Fatalf("second delay = %s, want 1h", got)
	}
	if got := garminRateLimitDelay(`{"retry_after":7200}`, 1); got != 2*time.Hour {
		t.Fatalf("server delay = %s, want 2h", got)
	}
}

func TestRecordGarminMutationRateLimitPersistsCircuit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "circuit.json")
	now := time.Date(2026, 7, 28, 3, 0, 0, 0, time.UTC)
	if err := recordGarminMutationRateLimitAt(path, `{"retry_after":30}`, now); err != nil {
		t.Fatalf("recordGarminMutationRateLimitAt() error = %v", err)
	}
	state, ok, err := loadGarminMutationCircuit(path)
	if err != nil {
		t.Fatalf("loadGarminMutationCircuit() error = %v", err)
	}
	if !ok || state.Consecutive429 != 1 || !state.BlockedUntil.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("state = %#v", state)
	}
}

func TestGarminMutationCircuitBlocksBeforeRequestWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "circuit.json")
	now := time.Date(2026, 7, 28, 3, 0, 0, 0, time.UTC)
	if err := recordGarminMutationRateLimitAt(path, `{"retry_after":30}`, now); err != nil {
		t.Fatalf("recordGarminMutationRateLimitAt() error = %v", err)
	}
	if err := checkGarminMutationCircuitAt(path, now.Add(29*time.Minute)); err == nil {
		t.Fatal("checkGarminMutationCircuitAt() error = nil before cooldown expiry")
	}
	if err := checkGarminMutationCircuitAt(path, now.Add(31*time.Minute)); err != nil {
		t.Fatalf("checkGarminMutationCircuitAt() after cooldown error = %v", err)
	}
}
