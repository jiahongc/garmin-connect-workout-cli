// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelHistoryCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "history",
		Short:       "history subcommands: search",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newNovelHistorySearchCmd(flags))
	return cmd
}
