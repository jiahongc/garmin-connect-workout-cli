// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

func newWorkoutsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "workouts",
		Short:       "Manage Garmin Connect workout templates",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newWorkoutsDeleteCmd(flags))
	cmd.AddCommand(newWorkoutsGetCmd(flags))
	cmd.AddCommand(newWorkoutsListCmd(flags))
	cmd.AddCommand(newWorkoutsReconcileCmd(flags))
	cmd.AddCommand(newWorkoutsTypesCmd(flags))
	cmd.AddCommand(newWorkoutsUploadJsonCmd(flags))
	cmd.AddCommand(newNovelWorkoutsApplyCmd(flags))
	cmd.AddCommand(newWorkoutsApplyBatchCmd(flags))
	cmd.AddCommand(newNovelWorkoutsPlanCmd(flags))
	cmd.AddCommand(newNovelWorkoutsSyncCheckCmd(flags))
	return cmd
}
