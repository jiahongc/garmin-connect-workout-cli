// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

func newScheduleGetCmd(flags *rootFlags) *cobra.Command {
	cmd := newGarminReadCmd(flags, "get <scheduled_workout_id>", "Get a scheduled workout by ID",
		"  garmin-connect-workout-cli schedule get 1801324592", "/workout-service/schedule/")
	cmd.Args = cobra.ExactArgs(1)
	return cmd
}
