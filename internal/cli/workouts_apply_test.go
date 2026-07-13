// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"io"
	"testing"

	"garmin-connect-workout-cli/internal/config"
)

func TestHasGarminWriteAuth(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{name: "nil config", cfg: nil, want: false},
		{name: "empty config", cfg: &config.Config{}, want: false},
		{name: "oauth header", cfg: &config.Config{AuthHeaderVal: "Bearer token"}, want: true},
		{name: "cookie-only session uses browser write", cfg: &config.Config{Headers: map[string]string{"Cookie": "SESSIONID=abc"}}, want: false},
		{name: "blank cookie header", cfg: &config.Config{Headers: map[string]string{"Cookie": "  "}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasGarminWriteAuth(tt.cfg); got != tt.want {
				t.Fatalf("hasGarminWriteAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNovelWorkoutsApplyHelpWires smoke-tests that the workouts apply command
// resolves at runtime and renders --help without error. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Always runs — do not delete this test when filling in real cases.
func TestNovelWorkoutsApplyHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"workouts", "apply", "--help"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workouts apply --help error = %v (novel command not wired correctly?)", err)
	}
}

// TestNovelWorkoutsApplyBehavior is the placeholder for table-driven tests of
// the workouts apply command's actual behavior. Replace the t.Skip with
// real cases — reviewers will flag a shipped t.Skip.
//
// Suggested shape:
//
//	func TestNovelWorkoutsApplyBehavior(t *testing.T) {
//	    cases := []struct {
//	        name  string
//	        input ...
//	        want  ...
//	    }{
//	        // {name: "...", input: ..., want: ...},
//	    }
//	    for _, tc := range cases {
//	        tc := tc
//	        t.Run(tc.name, func(t *testing.T) {
//	            t.Parallel()
//	            // assertions here
//	        })
//	    }
//	}
func TestNovelWorkoutsApplyBehavior(t *testing.T) {
	t.Skip("TODO: implement table-driven tests for workouts apply")
}
