// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"io"
	"testing"
)

// TestNovelPlanRaceBackwardHelpWires smoke-tests that the plan race-backward command
// resolves at runtime and renders --help without error. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Always runs — do not delete this test when filling in real cases.
func TestNovelPlanRaceBackwardHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"plan", "race-backward", "--help"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan race-backward --help error = %v (novel command not wired correctly?)", err)
	}
}

// TestNovelPlanRaceBackwardBehavior is the placeholder for table-driven tests of
// the plan race-backward command's actual behavior. Replace the t.Skip with
// real cases — reviewers will flag a shipped t.Skip.
//
// Suggested shape:
//
//	func TestNovelPlanRaceBackwardBehavior(t *testing.T) {
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
func TestNovelPlanRaceBackwardBehavior(t *testing.T) {
	t.Skip("TODO: implement table-driven tests for plan race-backward")
}
