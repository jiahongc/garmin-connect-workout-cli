// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newWorkoutsDeleteCmd(flags *rootFlags) *cobra.Command {
	return newGarminDeleteCmd(flags, "delete <workout_id>", "Delete a Garmin Connect workout template",
		"  garmin-connect-workout-cli workouts delete 1722206579 --apply", "/workout-service/workout/")
}

// newGarminDeleteCmd previews the DELETE by default and only sends it with --apply.
func newGarminDeleteCmd(flags *rootFlags, use, short, example, pathPrefix string) *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := pathPrefix + url.PathEscape(args[0])
			if !apply || flags.dryRun {
				preview := map[string]any{"dry_run": true, "apply": false, "method": "DELETE", "path": path, "next": "rerun with --apply to delete it from Garmin Connect"}
				return printJSONOrHuman(cmd, flags, preview, fmt.Sprintf("Dry run only. Rerun with --apply to DELETE %s.\n", path))
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Using the verified Garmin browser session for this write.")
			_, statusCode, err := garminBrowserDelete(cmd.Context(), path)
			if err != nil {
				return classifyDeleteError(err, flags)
			}
			result := map[string]any{"action": "delete", "path": path, "status": statusCode, "success": true}
			return printJSONOrHuman(cmd, flags, result, fmt.Sprintf("Deleted %s (HTTP %d).\n", path, statusCode))
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Actually delete from Garmin Connect")
	return cmd
}
