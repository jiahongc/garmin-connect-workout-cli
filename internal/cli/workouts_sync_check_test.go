// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"io"
	"testing"
)

// TestNovelWorkoutsSyncCheckHelpWires smoke-tests that the workouts sync-check command
// resolves at runtime and renders --help without error. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Always runs — do not delete this test when filling in real cases.
func TestNovelWorkoutsSyncCheckHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"workouts", "sync-check", "--help"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workouts sync-check --help error = %v (novel command not wired correctly?)", err)
	}
}

// TestNovelWorkoutsSyncCheckBehavior is the placeholder for table-driven tests of
// the workouts sync-check command's actual behavior. Replace the t.Skip with
// real cases — reviewers will flag a shipped t.Skip.
//
// Suggested shape:
//
//	func TestNovelWorkoutsSyncCheckBehavior(t *testing.T) {
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
func TestNovelWorkoutsSyncCheckBehavior(t *testing.T) {
	t.Skip("TODO: implement table-driven tests for workouts sync-check")
}
