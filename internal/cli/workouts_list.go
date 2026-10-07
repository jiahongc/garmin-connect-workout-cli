// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strconv"

	"github.com/spf13/cobra"
)

func newWorkoutsListCmd(flags *rootFlags) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List Garmin Connect workouts",
		Example: "  garmin-connect-workout-cli workouts list --limit 5",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, params := garminWorkoutsListRequest(limit)
			return runGarminRead(cmd, flags, path, params, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum workouts to return")
	return cmd
}

func garminWorkoutsListRequest(limit int) (string, map[string]string) {
	params := map[string]string{"start": "0"}
	if limit != 0 {
		params["limit"] = strconv.Itoa(limit)
	}
	return "/workout-service/workouts", params
}
