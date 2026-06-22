// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"

	"garmin-connect-workout-cli/internal/workoutdraft"
	"github.com/spf13/cobra"
)

func newNovelWorkoutsSyncCheckCmd(flags *rootFlags) *cobra.Command {
	var flagDate string

	cmd := &cobra.Command{
		Use:         "sync-check",
		Short:       "Check whether a workout is uploaded, scheduled, and ready for normal Garmin device sync.",
		Example:     "  garmin-connect-workout-cli workouts sync-check --date 2026-07-01 --json",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagDate != "" && !isISODate(flagDate) {
				return usageErr(fmt.Errorf("--date must use YYYY-MM-DD"))
			}
			if dryRunOK(flags) {
				fmt.Fprintln(cmd.OutOrStdout(), "would inspect local history and Garmin schedule state")
				return nil
			}
			store, err := workoutdraft.NewStore()
			if err != nil {
				return configErr(err)
			}
			drafts, err := store.List()
			if err != nil {
				return configErr(err)
			}
			var matches []workoutdraft.Draft
			for _, draft := range drafts {
				if flagDate == "" || draft.Date == flagDate || draft.ScheduledDate == flagDate {
					matches = append(matches, draft)
				}
			}
			ready := false
			for _, draft := range matches {
				if draft.UploadedWorkout != "" && (flagDate == "" || draft.ScheduledDate == flagDate || draft.Date == flagDate) {
					ready = true
					break
				}
			}
			out := map[string]any{
				"date":           flagDate,
				"ready_for_sync": ready,
				"checked_live":   false,
				"note":           "Local check only; after upload and schedule, sync the watch through Garmin Connect.",
				"workouts":       matches,
			}
			if flags.asJSON || flags.agent || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			if ready {
				fmt.Fprintln(cmd.OutOrStdout(), "Local history shows an uploaded and scheduled workout. Sync the device through Garmin Connect.")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "No uploaded/scheduled workout found locally for that date.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagDate, "date", "", "Schedule date in YYYY-MM-DD format")
	return cmd
}
