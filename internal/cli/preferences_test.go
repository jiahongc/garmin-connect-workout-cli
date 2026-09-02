// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"strings"
	"testing"

	"garmin-connect-workout-cli/internal/workoutprefs"
)

func TestWorkoutPreferenceQuestionnaireReturnsExplicitChoices(t *testing.T) {
	var output bytes.Buffer
	prefs, save, err := runWorkoutPreferenceQuestionnaire(
		strings.NewReader("3\n2\ny\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runWorkoutPreferenceQuestionnaire() error = %v", err)
	}
	if !save {
		t.Fatal("save = false, want true")
	}
	if prefs.StrideRecovery != "40 sec easy jog" || prefs.HillSprintRecovery != "walk down the hill" {
		t.Fatalf("preferences = %#v", prefs)
	}
	for _, want := range []string{"Stride recovery", "Hill-sprint recovery", "Save these preferences locally"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("questionnaire output = %q, want %q", output.String(), want)
		}
	}
}

func TestWorkoutPreferenceQuestionnaireSupportsTripleStrideDuration(t *testing.T) {
	prefs, save, err := runWorkoutPreferenceQuestionnaire(
		strings.NewReader("2\n1\ny\n"),
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !save || prefs.StrideRecovery != workoutprefs.TripleStrideDuration {
		t.Fatalf("preferences = %#v, save = %v", prefs, save)
	}
}

func TestWorkoutPreferenceQuestionnaireCanSaveAlwaysAsk(t *testing.T) {
	prefs, save, err := runWorkoutPreferenceQuestionnaire(
		strings.NewReader("1\n1\ny\n"),
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !save || prefs.StrideRecovery != workoutprefs.Ask || prefs.HillSprintRecovery != workoutprefs.Ask {
		t.Fatalf("preferences = %#v, save = %v", prefs, save)
	}
}

func TestWorkoutPreferenceQuestionnaireDoesNotPersistWithoutConsent(t *testing.T) {
	_, save, err := runWorkoutPreferenceQuestionnaire(
		strings.NewReader("3\n4\nn\n"),
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if save {
		t.Fatal("save = true, want false")
	}
}

func TestWorkoutPreferenceQuestionnaireRejectsInvalidCustomRecovery(t *testing.T) {
	_, _, err := runWorkoutPreferenceQuestionnaire(
		strings.NewReader("6\nwhenever\n"),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "time, distance, or manual recovery") {
		t.Fatalf("error = %v", err)
	}
}

func TestWorkoutsPlanUsesSavedStrideRecovery(t *testing.T) {
	t.Setenv("GARMIN_CONNECT_WORKOUTS_DATA_DIR", t.TempDir())
	if _, err := workoutprefs.Save(workoutprefs.Preferences{
		StrideRecovery:     "40 sec easy jog",
		HillSprintRecovery: workoutprefs.Ask,
	}); err != nil {
		t.Fatal(err)
	}

	cmd := RootCmd()
	cmd.SetArgs([]string{"--json", "workouts", "plan", "35min easy + 4x20s relaxed strides", "--date", "2026-09-01"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workouts plan error = %v", err)
	}
	if !strings.Contains(stdout.String(), `"duration_seconds": 40`) || !strings.Contains(stdout.String(), `"step_type": "recovery"`) {
		t.Fatalf("plan output = %s", stdout.String())
	}
}

func TestWorkoutsPlanStillAsksForDistanceRangeWithSavedRecovery(t *testing.T) {
	t.Setenv("GARMIN_CONNECT_WORKOUTS_DATA_DIR", t.TempDir())
	if _, err := workoutprefs.Save(workoutprefs.Preferences{
		StrideRecovery:     "40 sec easy jog",
		HillSprintRecovery: workoutprefs.Ask,
	}); err != nil {
		t.Fatal(err)
	}

	cmd := RootCmd()
	cmd.SetArgs([]string{"--agent", "workouts", "plan", "6-7mi easy + 6x20s relaxed strides", "--date", "2026-09-01"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "exact total distance") {
		t.Fatalf("workouts plan error = %v", err)
	}
}
