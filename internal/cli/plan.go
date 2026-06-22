// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelPlanCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "plan",
		Short:       "plan subcommands: race-backward",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newNovelPlanRaceBackwardCmd(flags))
	return cmd
}
