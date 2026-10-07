// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

func newWorkoutsTypesCmd(flags *rootFlags) *cobra.Command {
	cmd := newGarminReadCmd(flags, "types", "List Garmin workout type metadata",
		"  garmin-connect-workout-cli workouts types", "/workout-service/workout/types")
	cmd.Args = cobra.NoArgs
	return cmd
}
