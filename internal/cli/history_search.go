// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"garmin-connect-workout-cli/internal/workoutdraft"
	"github.com/spf13/cobra"
)

func newNovelHistorySearchCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "search [query]",
		Short:       "Find prior authored workouts by prompt, pace target, interval shape, date, or Garmin ID.",
		Example:     "  garmin-connect-workout-cli history search 800m --json --select workouts.name,workouts.date,workouts.garmin_id",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				fmt.Fprintln(cmd.OutOrStdout(), "would search local workout history")
				return nil
			}
			store, err := workoutdraft.NewStore()
			if err != nil {
				return configErr(err)
			}
			results, err := store.Search(strings.Join(args, " "))
			if err != nil {
				return configErr(err)
			}
			out := map[string]any{
				"count":        len(results),
				"history_path": store.Path,
				"workouts":     results,
			}
			if flags.asJSON || flags.agent || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			if len(results) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No local workout drafts found at %s\n", store.Path)
				return nil
			}
			for _, draft := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", draft.ID, draft.Name, draft.Date, draft.UploadedWorkout)
			}
			return nil
		},
	}
	return cmd
}
