// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestNovelWorkoutsPlanHelpWires smoke-tests that the workouts plan command
// resolves at runtime and renders --help without error. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Always runs — do not delete this test when filling in real cases.
func TestNovelWorkoutsPlanHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"workouts", "plan", "--help"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workouts plan --help error = %v (novel command not wired correctly?)", err)
	}
}

func TestResolveWorkoutClarificationsReturnsClearPromptUnchanged(t *testing.T) {
	prompt := "10 min warmup, 6x800m at 5K pace with 2 min jog, 10 min cooldown"
	got, err := resolveWorkoutClarifications(strings.NewReader(""), io.Discard, prompt, false)
	if err != nil {
		t.Fatalf("resolveWorkoutClarifications() error = %v", err)
	}
	if got != prompt {
		t.Fatalf("resolved prompt = %q, want %q", got, prompt)
	}
}

func TestResolveWorkoutClarificationsAsksForCompleteRevision(t *testing.T) {
	original := "10 min warmup, 6x800m at 5K pace, 10 min cooldown"
	revised := "10 min warmup, 6x800m at 5K pace with 2 min jog, 10 min cooldown"
	var questions bytes.Buffer

	got, err := resolveWorkoutClarifications(
		strings.NewReader(revised+"\n"),
		&questions,
		original,
		true,
	)
	if err != nil {
		t.Fatalf("resolveWorkoutClarifications() error = %v", err)
	}
	if got != revised {
		t.Fatalf("resolved prompt = %q, want %q", got, revised)
	}
	for _, want := range []string{"Before I create this workout", "recovery", "Enter the complete workout"} {
		if !strings.Contains(questions.String(), want) {
			t.Fatalf("questions = %q, want substring %q", questions.String(), want)
		}
	}
}

func TestResolveWorkoutClarificationsDoesNotGuessWithoutInput(t *testing.T) {
	_, err := resolveWorkoutClarifications(
		strings.NewReader(""),
		io.Discard,
		"6x800m at 5K pace",
		false,
	)
	if err == nil {
		t.Fatal("resolveWorkoutClarifications() error = nil, want clarification error")
	}
	for _, want := range []string{"workout needs clarification", "recovery", "rerun"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error = %q, want substring %q", err, want)
		}
	}
}
