// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newWorkoutsUploadJsonCmd(flags *rootFlags) *cobra.Command {
	var workoutJSON string
	var stdinBody bool
	var apply bool
	cmd := &cobra.Command{
		Use:     "upload-json",
		Short:   "Upload a raw Garmin workout JSON payload",
		Example: "  garmin-connect-workout-cli workouts upload-json --workout-json '{...}' --apply",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if workoutJSON == "" && !stdinBody {
				return usageErr(fmt.Errorf("pass --workout-json or --stdin"))
			}
			body, err := readJSONObject(cmd, workoutJSON, stdinBody)
			if err != nil {
				return err
			}
			return runGarminWrite(cmd, flags, apply, "/workout-service/workout", "garmin_payload", body, "upload this raw workout JSON")
		},
	}
	cmd.Flags().StringVar(&workoutJSON, "workout-json", "", "Raw Garmin workout JSON object")
	cmd.Flags().BoolVar(&stdinBody, "stdin", false, "Read request body as JSON from stdin")
	cmd.Flags().BoolVar(&apply, "apply", false, "Actually upload the raw workout JSON to Garmin Connect")
	return cmd
}

// readJSONObject parses raw, or stdin when fromStdin is set, as a JSON object.
func readJSONObject(cmd *cobra.Command, raw string, fromStdin bool) (map[string]any, error) {
	data := []byte(raw)
	if fromStdin {
		var err error
		if data, err = io.ReadAll(cmd.InOrStdin()); err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, usageErr(fmt.Errorf("parsing request JSON: %w", err))
	}
	if body == nil {
		return nil, usageErr(fmt.Errorf("request body must be a JSON object"))
	}
	return body, nil
}

// runGarminWrite previews the POST by default and sends it through the
// browser session only with --apply.
func runGarminWrite(cmd *cobra.Command, flags *rootFlags, apply bool, path, bodyKey string, body map[string]any, action string) error {
	if !apply || flags.dryRun {
		preview := map[string]any{"dry_run": true, "apply": false, "method": "POST", "path": path, bodyKey: body, "next": "rerun with --apply to " + action}
		return printJSONOrHuman(cmd, flags, preview, "Dry run only. Rerun with --apply to "+action+".\n")
	}
	data, statusCode, err := postGarminWorkout(cmd, flags, path, body)
	if err != nil {
		return classifyAPIError(err, flags)
	}
	result := map[string]any{"path": path, "status": statusCode, "success": true}
	var parsed any
	if json.Unmarshal(data, &parsed) == nil {
		result["data"] = parsed
	}
	return printJSONOrHuman(cmd, flags, result, fmt.Sprintf("Done: POST %s (HTTP %d).\n", path, statusCode))
}
