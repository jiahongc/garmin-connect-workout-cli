// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package workoutprefs

import (
	"os"
	"testing"
)

func TestLoadReturnsAskDefaultsWhenPreferencesDoNotExist(t *testing.T) {
	t.Setenv("GARMIN_CONNECT_WORKOUTS_DATA_DIR", t.TempDir())

	prefs, path, found, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if found {
		t.Fatal("Load() found preferences, want false")
	}
	if path == "" || prefs.StrideRecovery != Ask || prefs.HillSprintRecovery != Ask {
		t.Fatalf("Load() = %#v, path %q", prefs, path)
	}
}

func TestPreferencesRoundTripInPrivateLocalFile(t *testing.T) {
	t.Setenv("GARMIN_CONNECT_WORKOUTS_DATA_DIR", t.TempDir())
	want := Preferences{
		Version:            CurrentVersion,
		StrideRecovery:     "40 sec easy jog",
		HillSprintRecovery: "walk down the hill",
	}

	path, err := Save(want)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("preference file mode = %o, want 600", got)
	}

	got, gotPath, found, err := Load()
	if err != nil || !found {
		t.Fatalf("Load() found = %v, error = %v", found, err)
	}
	if gotPath != path || got != want {
		t.Fatalf("Load() = %#v, path %q; want %#v, path %q", got, gotPath, want, path)
	}
}

func TestSaveNormalizesBlankRecoveriesToAsk(t *testing.T) {
	t.Setenv("GARMIN_CONNECT_WORKOUTS_DATA_DIR", t.TempDir())
	if _, err := Save(Preferences{}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	prefs, _, _, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if prefs.Version != CurrentVersion || prefs.StrideRecovery != Ask || prefs.HillSprintRecovery != Ask {
		t.Fatalf("normalized preferences = %#v", prefs)
	}
}
