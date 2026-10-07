// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

func newWorkoutsGetCmd(flags *rootFlags) *cobra.Command {
	cmd := newGarminReadCmd(flags, "get <workout_id>", "Get a Garmin Connect workout by ID",
		"  garmin-connect-workout-cli workouts get 1722206579", "/workout-service/workout/")
	cmd.Args = cobra.ExactArgs(1)
	return cmd
}
