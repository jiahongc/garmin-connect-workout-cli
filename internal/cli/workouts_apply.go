// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"garmin-connect-workout-cli/internal/workoutdraft"
	"github.com/spf13/cobra"
)

func newNovelWorkoutsApplyCmd(flags *rootFlags) *cobra.Command {
	var flagSchedule string
	var flagNoSchedule bool
	var flagApply bool

	cmd := &cobra.Command{
		Use:     "apply <draft-id>",
		Short:   "Upload or update a generated workout only after showing the exact payload diff.",
		Example: "  garmin-connect-workout-cli workouts apply draft_4x800 --apply --json\n  garmin-connect-workout-cli workouts apply draft_4x800 --schedule 2026-07-01 --apply --json\n  garmin-connect-workout-cli workouts apply draft_4x800 --no-schedule --apply",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) == 0 {
				return usageErr(fmt.Errorf("draft id is required"))
			}
			if flagSchedule != "" && !isISODate(flagSchedule) {
				return usageErr(fmt.Errorf("--schedule must use YYYY-MM-DD"))
			}
			if flagSchedule != "" && flagNoSchedule {
				return usageErr(fmt.Errorf("--schedule and --no-schedule cannot be used together"))
			}
			if dryRunOK(flags) {
				fmt.Fprintln(cmd.OutOrStdout(), "would upload the draft workout and optionally schedule it")
				return nil
			}
			store, err := workoutdraft.NewStore()
			if err != nil {
				return configErr(err)
			}
			draft, err := store.Get(args[0])
			if err != nil {
				return notFoundErr(err)
			}
			if flagSchedule == "" && !flagNoSchedule {
				flagSchedule = draft.Date
			}
			preview := map[string]any{
				"draft_id":       draft.ID,
				"apply":          flagApply,
				"schedule":       flagSchedule,
				"garmin_payload": draft.GarminPayload,
			}
			if !flagApply {
				preview["dry_run"] = true
				preview["next"] = "rerun with --apply to upload this workout to Garmin Connect"
				return printJSONOrHuman(cmd, flags, preview, fmt.Sprintf("Dry run only. Rerun with --apply to upload draft %s.\n", draft.ID))
			}
			data, statusCode, err := postGarminWorkout(cmd, flags, "/workout-service/workout", draft.GarminPayload)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			workoutID := extractResponseID(data, "workoutId", "workout_id", "id")
			if workoutID == "" {
				return apiErr(fmt.Errorf("Garmin upload returned HTTP %d without workoutId: %s", statusCode, strings.TrimSpace(string(data))))
			}
			result := map[string]any{
				"draft_id":   draft.ID,
				"uploaded":   true,
				"workout_id": workoutID,
				"status":     statusCode,
			}
			scheduledID := ""
			if flagSchedule != "" {
				scheduleBody := map[string]string{"date": flagSchedule}
				scheduleData, scheduleStatus, err := postGarminWorkout(cmd, flags, "/workout-service/schedule/"+workoutID, scheduleBody)
				if err != nil {
					return classifyAPIError(err, flags)
				}
				scheduledID = extractResponseID(scheduleData, "workoutScheduleId", "scheduledWorkoutId", "id")
				result["scheduled"] = true
				result["schedule_status"] = scheduleStatus
				result["scheduled_workout_id"] = scheduledID
				result["scheduled_date"] = flagSchedule
				result["schedule_response"] = json.RawMessage(scheduleData)
			}
			if err := store.MarkApplied(draft.ID, workoutID, scheduledID, flagSchedule); err != nil {
				return configErr(fmt.Errorf("updating local history: %w", err))
			}
			return printJSONOrHuman(cmd, flags, result, fmt.Sprintf("Uploaded workout %s\n", workoutID))
		},
	}
	cmd.Flags().StringVar(&flagSchedule, "schedule", "", "Schedule date in YYYY-MM-DD format; defaults to the draft date")
	cmd.Flags().BoolVar(&flagNoSchedule, "no-schedule", false, "Upload the workout without adding it to the Garmin calendar")
	cmd.Flags().BoolVar(&flagApply, "apply", false, "Actually write the workout to Garmin Connect")
	return cmd
}

func postGarminWorkout(cmd *cobra.Command, flags *rootFlags, path string, body any) ([]byte, int, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, 0, err
	}
	if c.Config == nil || c.Config.AuthHeader() == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Using signed-in Garmin browser profile for this write.")
		return garminBrowserPostJSON(cmd.Context(), path, body)
	}
	return c.Post(cmd.Context(), path, body)
}

func extractResponseID(data []byte, keys ...string) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch typed := v.(type) {
			case string:
				return typed
			case float64:
				return fmt.Sprintf("%.0f", typed)
			}
		}
	}
	return ""
}
