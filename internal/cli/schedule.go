// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

func newScheduleCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "schedule",
		Short:       "Manage Garmin Connect scheduled workouts",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newScheduleCreateCmd(flags))
	cmd.AddCommand(newScheduleDeleteCmd(flags))
	cmd.AddCommand(newScheduleGetCmd(flags))
	return cmd
}
