// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newScheduleCreateCmd(flags *rootFlags) *cobra.Command {
	var date string
	var stdinBody bool
	var apply bool
	cmd := &cobra.Command{
		Use:     "create <workout_id>",
		Short:   "Schedule a workout on a Garmin Connect calendar date",
		Example: "  garmin-connect-workout-cli schedule create 1722206579 --date 2026-10-07 --apply",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"date": date}
			if stdinBody {
				var err error
				if body, err = readJSONObject(cmd, "", true); err != nil {
					return err
				}
			} else if date == "" {
				return usageErr(fmt.Errorf("--date is required"))
			}
			path := "/workout-service/schedule/" + url.PathEscape(args[0])
			return runGarminWrite(cmd, flags, apply, path, "body", body, "schedule this workout in Garmin Connect")
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "Schedule date in YYYY-MM-DD format")
	cmd.Flags().BoolVar(&stdinBody, "stdin", false, "Read request body as JSON from stdin")
	cmd.Flags().BoolVar(&apply, "apply", false, "Actually schedule the workout in Garmin Connect")
	return cmd
}
