// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"garmin-connect-workout-cli/internal/config"
	"garmin-connect-workout-cli/internal/garminsession"
	"garmin-connect-workout-cli/internal/workoutdraft"
	"github.com/spf13/cobra"
)

func newNovelWorkoutsApplyCmd(flags *rootFlags) *cobra.Command {
	var flagSchedule string
	var flagNoSchedule bool
	var flagApply bool
	var flagReplace string

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
			flagSchedule = resolveWorkoutSchedule(flagSchedule, flagNoSchedule, draft.Date, flagReplace)
			method, path := garminWorkoutWriteTarget(flagReplace)
			preview := map[string]any{
				"draft_id":       draft.ID,
				"apply":          flagApply,
				"schedule":       flagSchedule,
				"method":         method,
				"path":           path,
				"garmin_payload": draft.GarminPayload,
			}
			if !flagApply {
				preview["dry_run"] = true
				preview["next"] = "rerun with --apply to upload this workout to Garmin Connect"
				return printJSONOrHuman(cmd, flags, preview, fmt.Sprintf("Dry run only. Rerun with --apply to upload draft %s.\n", draft.ID))
			}
			result, err := applyGarminWorkoutDraft(cmd, flags, store, draft, method, path, flagSchedule, flagReplace)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printJSONOrHuman(cmd, flags, result, fmt.Sprintf("Uploaded workout %v\n", result["workout_id"]))
		},
	}
	cmd.Flags().StringVar(&flagSchedule, "schedule", "", "Schedule date in YYYY-MM-DD format; defaults to the draft date")
	cmd.Flags().BoolVar(&flagNoSchedule, "no-schedule", false, "Upload the workout without adding it to the Garmin calendar")
	cmd.Flags().BoolVar(&flagApply, "apply", false, "Actually write the workout to Garmin Connect")
	cmd.Flags().StringVar(&flagReplace, "replace", "", "Update an existing Garmin workout ID in place")
	return cmd
}

func applyGarminWorkoutDraft(
	cmd *cobra.Command,
	flags *rootFlags,
	store workoutdraft.Store,
	draft workoutdraft.Draft,
	method, path, scheduleDate, replaceID string,
) (map[string]any, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, err
	}
	if useGarminBrowserMutationSession(c.Config) {
		return applyGarminWorkoutDraftWithBrowser(cmd, flags, store, draft, scheduleDate, replaceID)
	}

	var data []byte
	var statusCode int
	if method == "PUT" {
		data, statusCode, err = c.Put(cmd.Context(), path, draft.GarminPayload)
	} else {
		data, statusCode, err = c.Post(cmd.Context(), path, draft.GarminPayload)
	}
	if err != nil {
		return nil, err
	}
	workoutID := extractResponseID(data, "workoutId", "workout_id", "id")
	if workoutID == "" {
		workoutID = replaceID
	}
	if workoutID == "" {
		return nil, apiErr(fmt.Errorf("Garmin upload returned HTTP %d without workoutId: %s", statusCode, strings.TrimSpace(string(data))))
	}
	result := workoutApplyResult(draft.ID, workoutID, statusCode, replaceID)
	if err := store.MarkApplied(draft.ID, workoutID, "", ""); err != nil {
		return nil, configErr(fmt.Errorf("checkpointing uploaded workout: %w", err))
	}
	scheduledID := ""
	if scheduleDate != "" {
		scheduleData, scheduleStatus, err := c.Post(cmd.Context(), "/workout-service/schedule/"+workoutID, map[string]string{"date": scheduleDate})
		if err != nil {
			return nil, err
		}
		scheduledID = extractResponseID(scheduleData, "workoutScheduleId", "scheduledWorkoutId", "id")
		addWorkoutScheduleResult(result, scheduleDate, scheduledID, scheduleStatus, scheduleData)
	}
	if scheduleDate != "" {
		if err := store.MarkApplied(draft.ID, workoutID, scheduledID, scheduleDate); err != nil {
			return nil, configErr(fmt.Errorf("checkpointing scheduled workout: %w", err))
		}
	}
	return result, nil
}

func workoutApplyResult(draftID, workoutID string, statusCode int, replaceID string) map[string]any {
	result := map[string]any{"draft_id": draftID, "workout_id": workoutID, "status": statusCode}
	if replaceID == "" {
		result["uploaded"] = true
	} else {
		result["updated"] = true
	}
	return result
}

func addWorkoutScheduleResult(result map[string]any, date, scheduledID string, statusCode int, data []byte) {
	result["scheduled"] = true
	result["schedule_status"] = statusCode
	result["scheduled_workout_id"] = scheduledID
	result["scheduled_date"] = date
	result["schedule_response"] = json.RawMessage(data)
}

func applyGarminWorkoutDraftWithBrowser(
	cmd *cobra.Command,
	flags *rootFlags,
	store workoutdraft.Store,
	draft workoutdraft.Draft,
	scheduleDate, replaceID string,
) (map[string]any, error) {
	if err := checkGarminMutationCircuit(time.Now()); err != nil {
		return nil, err
	}
	profileDir, profileReady, err := garminsession.BrowserProfileReady()
	if err != nil {
		return nil, configErr(err)
	}
	if !profileReady {
		return nil, authErr(fmt.Errorf("Garmin browser profile is not ready; run auth login-browser first"))
	}
	webSession, _, found, err := garminsession.Load()
	if err != nil {
		return nil, configErr(err)
	}
	if !found || !webSession.Active(time.Now()) {
		return nil, authErr(fmt.Errorf("Garmin web session is not verified; run auth login-browser once"))
	}

	fmt.Fprintln(cmd.ErrOrStderr(), "Reusing the verified Garmin session headlessly for upload and scheduling.")
	var result map[string]any
	err = runGarminSingleApplyBrowser(cmd.Context(), profileDir, *webSession, func(browserCtx context.Context) error {
		session := newGarminBrowserMutationSession(browserCtx)
		if err := session.discoverBase(); err != nil {
			return err
		}
		var applyErr error
		result, applyErr = applyGarminWorkoutDraftWithMutationSession(
			browserCtx,
			store,
			draft,
			scheduleDate,
			replaceID,
			session,
			defaultGarminBatchMutationDelay,
		)
		return applyErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func applyGarminWorkoutDraftWithMutationSession(
	ctx context.Context,
	store workoutdraft.Store,
	draft workoutdraft.Draft,
	scheduleDate, replaceID string,
	session *garminBrowserMutationSession,
	mutationDelay time.Duration,
) (map[string]any, error) {
	lastMutation := time.Time{}
	mutate := garminBatchMutateFunc(func(method, path string, body any, verify garminMutationVerifier) (browserPostResponse, error) {
		if !lastMutation.IsZero() {
			if err := waitForGarminMutationSpacing(ctx, time.Until(lastMutation.Add(mutationDelay))); err != nil {
				return browserPostResponse{}, err
			}
		}
		response, err := session.mutate(method, path, body, verify)
		lastMutation = time.Now()
		return response, err
	})

	itemDraft := draft
	if replaceID != "" {
		itemDraft.UploadedWorkout = replaceID
	}
	liveWorkouts, err := listGarminWorkoutsWithMutationSession(session, defaultGarminBatchListLimit)
	if err != nil {
		return nil, err
	}
	itemFlags := rootFlags{}
	itemFlags.idempotent = true
	batchResult, _, err := applyWorkoutBatchItem(
		ctx,
		&itemFlags,
		store,
		session,
		mutate,
		workoutApplyBatchItem{Draft: itemDraft, Schedule: scheduleDate},
		liveWorkouts,
		replaceID != "",
	)
	if err != nil {
		return nil, err
	}
	workoutID, _ := batchResult["workout_id"].(string)
	uploadHTTPStatus, _ := batchResult["upload_http_status"].(int)
	result := workoutApplyResult(draft.ID, workoutID, uploadHTTPStatus, replaceID)
	result["upload_status"] = batchResult["upload_status"]
	if scheduleDate != "" {
		scheduledID, _ := batchResult["scheduled_workout_id"].(string)
		scheduleHTTPStatus, _ := batchResult["schedule_http_status"].(int)
		body, _ := json.Marshal(map[string]string{"workoutScheduleId": scheduledID})
		addWorkoutScheduleResult(result, scheduleDate, scheduledID, scheduleHTTPStatus, body)
		result["schedule_disposition"] = batchResult["schedule_status"]
	}
	return result, nil
}

func garminWorkoutWriteTarget(replaceID string) (string, string) {
	if replaceID != "" {
		return "PUT", "/workout-service/workout/" + replaceID
	}
	return "POST", "/workout-service/workout"
}

func resolveWorkoutSchedule(requested string, noSchedule bool, draftDate, replaceID string) string {
	if noSchedule {
		return ""
	}
	if requested != "" {
		return requested
	}
	if replaceID != "" {
		return ""
	}
	return draftDate
}

func postGarminWorkout(cmd *cobra.Command, flags *rootFlags, path string, body any) ([]byte, int, error) {
	return mutateGarminWorkout(cmd, flags, "POST", path, body)
}

func mutateGarminWorkout(cmd *cobra.Command, flags *rootFlags, method, path string, body any) ([]byte, int, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, 0, err
	}
	if useGarminBrowserMutationSession(c.Config) {
		fmt.Fprintln(cmd.ErrOrStderr(), "Using the verified Garmin browser session headlessly for this write.")
		if method == "PUT" {
			return garminBrowserPutJSON(cmd.Context(), path, body)
		}
		return garminBrowserPostJSON(cmd.Context(), path, body)
	}
	if method == "PUT" {
		return c.Put(cmd.Context(), path, body)
	}
	return c.Post(cmd.Context(), path, body)
}

func useGarminBrowserMutationSession(cfg *config.Config) bool {
	return cfg == nil || cfg.AuthSource == "garmin-web-session" || !hasGarminWriteAuth(cfg)
}

func runGarminSingleApplyBrowser(parent context.Context, profileDir string, webSession garminsession.Session, action func(context.Context) error) error {
	if err := runGarminBrowserWithSession(parent, profileDir, webSession, true, 90*time.Second, action); err != nil {
		return fmt.Errorf("running Garmin write through the saved browser profile: %w", err)
	}
	return nil
}

func hasGarminWriteAuth(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return strings.TrimSpace(cfg.AuthHeader()) != ""
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
