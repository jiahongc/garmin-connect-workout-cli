// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

func newScheduleDeleteCmd(flags *rootFlags) *cobra.Command {
	return newGarminDeleteCmd(flags, "delete <scheduled_workout_id>", "Remove a scheduled workout from the calendar",
		"  garmin-connect-workout-cli schedule delete 1801324592 --apply", "/workout-service/schedule/")
}
