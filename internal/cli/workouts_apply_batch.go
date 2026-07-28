// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"garmin-connect-workout-cli/internal/garminsession"
	"garmin-connect-workout-cli/internal/types"
	"garmin-connect-workout-cli/internal/workoutdraft"
	"github.com/spf13/cobra"
)

const (
	defaultGarminBatchMutationDelay = 10 * time.Second
	defaultGarminBatchLoginTimeout  = 8 * time.Minute
	defaultGarminBatchListLimit     = 100
)

type workoutApplyBatchItem struct {
	Draft    workoutdraft.Draft
	Schedule string
}

type garminBatchMutateFunc func(string, string, any, garminMutationVerifier) (browserPostResponse, error)

func newWorkoutsApplyBatchCmd(flags *rootFlags) *cobra.Command {
	var apply bool
	var noSchedule bool
	var replaceUploaded bool
	var loginTimeout time.Duration
	var mutationDelay time.Duration

	cmd := &cobra.Command{
		Use:   "apply-batch <draft-id> [draft-id...]",
		Short: "Upload and schedule multiple drafts in one guarded browser session",
		Long: `Uploads or replaces multiple saved drafts through one visible signed-in browser session.

The command discovers a working Garmin backend with a read-only request before
the first mutation, checkpoints each successful upload before scheduling it,
verifies workout payloads and calendar placement, and stops the complete batch
on HTTP 429.`,
		Example: strings.Join([]string{
			"  garmin-connect-workout-cli workouts apply-batch draft_a draft_b --json",
			"  garmin-connect-workout-cli workouts apply-batch draft_a draft_b --apply --yes --idempotent --json",
			"  garmin-connect-workout-cli workouts apply-batch draft_a draft_b --replace-uploaded --apply --yes --idempotent --json",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if loginTimeout <= 0 {
				return usageErr(fmt.Errorf("--login-timeout must be positive"))
			}
			if mutationDelay < defaultGarminBatchMutationDelay {
				return usageErr(fmt.Errorf("--mutation-delay must be at least %s", defaultGarminBatchMutationDelay))
			}
			if apply && !flags.yes {
				return usageErr(fmt.Errorf("--apply requires --yes"))
			}
			store, err := workoutdraft.NewStore()
			if err != nil {
				return configErr(err)
			}
			items, err := loadWorkoutApplyBatchItems(store, args, noSchedule)
			if err != nil {
				return err
			}
			preview := workoutApplyBatchPreview(items, apply, replaceUploaded, mutationDelay)
			if !apply {
				preview["dry_run"] = true
				preview["next"] = "review every payload, then rerun with --apply --yes"
				return printJSONOrHuman(cmd, flags, preview, fmt.Sprintf("Dry run only. Reviewed %d drafts; rerun with --apply --yes.\n", len(items)))
			}
			if err := checkGarminMutationCircuit(time.Now()); err != nil {
				return err
			}

			profileDir, err := garminsession.BrowserProfileDir()
			if err != nil {
				return configErr(err)
			}
			if err := os.MkdirAll(profileDir, 0o700); err != nil {
				return configErr(fmt.Errorf("creating browser profile dir: %w", err))
			}

			results := make([]map[string]any, 0, len(items))
			fmt.Fprintln(cmd.ErrOrStderr(), "Opening one visible Garmin browser session for the complete workout batch.")
			fmt.Fprintln(cmd.ErrOrStderr(), "If Garmin asks, sign in and complete MFA in that browser window.")
			_, err = verifyGarminBrowserProfileWithAction(cmd.Context(), profileDir, loginTimeout, func(browserCtx context.Context) error {
				session := newGarminBrowserMutationSession(browserCtx)
				if err := session.discoverBase(); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Using Garmin backend %s for this session.\n", session.base)

				liveWorkouts, err := listGarminWorkoutsWithMutationSession(session, defaultGarminBatchListLimit)
				if err != nil {
					return err
				}
				lastMutation := time.Time{}
				mutate := func(method, path string, body any, verify garminMutationVerifier) (browserPostResponse, error) {
					if !lastMutation.IsZero() {
						if err := waitForGarminMutationSpacing(browserCtx, time.Until(lastMutation.Add(mutationDelay))); err != nil {
							return browserPostResponse{}, err
						}
					}
					response, err := session.mutate(method, path, body, verify)
					lastMutation = time.Now()
					return response, err
				}

				for index, item := range items {
					result, updatedLive, err := applyWorkoutBatchItem(
						browserCtx,
						flags,
						store,
						session,
						mutate,
						item,
						liveWorkouts,
						replaceUploaded,
					)
					if err != nil {
						return fmt.Errorf("applying draft %s (%d/%d): %w", item.Draft.ID, index+1, len(items), err)
					}
					liveWorkouts = updatedLive
					results = append(results, result)
					fmt.Fprintf(cmd.ErrOrStderr(), "Completed %d/%d: %s on %s.\n", index+1, len(items), item.Draft.Name, item.Schedule)
				}
				return nil
			})
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
				"applied":          true,
				"browser_sessions": 1,
				"results":          results,
				"verified":         true,
			}, flags)
		},
	}

	cmd.Flags().BoolVar(&apply, "apply", false, "Actually upload and schedule every draft")
	cmd.Flags().BoolVar(&noSchedule, "no-schedule", false, "Upload every workout without adding it to the calendar")
	cmd.Flags().BoolVar(&replaceUploaded, "replace-uploaded", false, "Update each draft's checkpointed Garmin workout in place and verify the live payload")
	cmd.Flags().DurationVar(&loginTimeout, "login-timeout", defaultGarminBatchLoginTimeout, "Maximum time for login and batch application")
	cmd.Flags().DurationVar(&mutationDelay, "mutation-delay", defaultGarminBatchMutationDelay, "Minimum spacing between Garmin mutations")
	return cmd
}

func loadWorkoutApplyBatchItems(store workoutdraft.Store, ids []string, noSchedule bool) ([]workoutApplyBatchItem, error) {
	seen := map[string]struct{}{}
	items := make([]workoutApplyBatchItem, 0, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			return nil, usageErr(fmt.Errorf("duplicate draft id %q", id))
		}
		seen[id] = struct{}{}
		draft, err := store.Get(id)
		if err != nil {
			return nil, notFoundErr(err)
		}
		schedule := draft.Date
		if noSchedule {
			schedule = ""
		} else if schedule == "" {
			return nil, usageErr(fmt.Errorf("draft %q has no date; use --no-schedule or create a dated draft", id))
		}
		items = append(items, workoutApplyBatchItem{Draft: draft, Schedule: schedule})
	}
	return items, nil
}

func workoutApplyBatchPreview(items []workoutApplyBatchItem, apply bool, replaceUploaded bool, mutationDelay time.Duration) map[string]any {
	drafts := make([]map[string]any, 0, len(items))
	for _, item := range items {
		drafts = append(drafts, map[string]any{
			"draft_id":       item.Draft.ID,
			"name":           item.Draft.Name,
			"schedule":       item.Schedule,
			"garmin_payload": item.Draft.GarminPayload,
		})
	}
	return map[string]any{
		"apply":            apply,
		"browser_sessions": 1,
		"draft_count":      len(items),
		"drafts":           drafts,
		"mutation_delay":   mutationDelay.String(),
		"replace_uploaded": replaceUploaded,
	}
}

func applyWorkoutBatchItem(
	ctx context.Context,
	flags *rootFlags,
	store workoutdraft.Store,
	session *garminBrowserMutationSession,
	mutate garminBatchMutateFunc,
	item workoutApplyBatchItem,
	liveWorkouts []types.Workout,
	replaceUploaded bool,
) (map[string]any, []types.Workout, error) {
	workoutID := item.Draft.UploadedWorkout
	uploadStatus := "created"
	if workoutID != "" {
		if !containsGarminWorkoutID(liveWorkouts, workoutID) {
			return nil, liveWorkouts, fmt.Errorf("local draft points to Garmin workout %s, but it is absent from the live library", workoutID)
		}
		uploadStatus = "checkpointed"
		if replaceUploaded {
			path := "/workout-service/workout/" + workoutID
			response, err := mutate("PUT", path, item.Draft.GarminPayload, func() (browserPostResponse, bool, error) {
				verified, matches, _, err := verifyGarminWorkoutPayloadWithMutationSession(session, workoutID, item.Draft.GarminPayload)
				return verified, matches, err
			})
			if err != nil {
				return nil, liveWorkouts, err
			}
			_, matches, mismatch, err := verifyGarminWorkoutPayloadWithMutationSession(session, workoutID, item.Draft.GarminPayload)
			if err != nil {
				return nil, liveWorkouts, err
			}
			if !matches {
				return nil, liveWorkouts, fmt.Errorf("Garmin accepted the update for workout %s, but live verification failed: %s", workoutID, mismatch)
			}
			uploadStatus = "updated"
			if response.BaseURL == "verified-live-state" {
				uploadStatus = "recovered"
			}
		}
	} else {
		matches := exactGarminWorkoutNameMatches(liveWorkouts, item.Draft.Name)
		if len(matches) > 0 {
			if len(matches) != 1 || !flags.idempotent {
				hint := "use a unique name"
				if len(matches) == 1 {
					hint = "pass --idempotent to reuse the exact match"
				}
				return nil, liveWorkouts, fmt.Errorf(
					"workout name %q matched %d live workouts; %s",
					item.Draft.Name,
					len(matches),
					hint,
				)
			}
			workoutID = matches[0].WorkoutId
			uploadStatus = "existing"
		} else {
			response, err := mutate("POST", "/workout-service/workout", item.Draft.GarminPayload, func() (browserPostResponse, bool, error) {
				current, err := listGarminWorkoutsWithMutationSession(session, defaultGarminBatchListLimit)
				if err != nil {
					return browserPostResponse{}, false, err
				}
				matches := exactGarminWorkoutNameMatches(current, item.Draft.Name)
				if len(matches) > 1 {
					return browserPostResponse{}, false, fmt.Errorf("workout name %q matched %d live workouts after HTTP 427", item.Draft.Name, len(matches))
				}
				if len(matches) == 0 {
					return browserPostResponse{}, false, nil
				}
				body, _ := json.Marshal(map[string]string{"workoutId": matches[0].WorkoutId})
				return browserPostResponse{BaseURL: "verified-live-state", Status: 200, Body: string(body)}, true, nil
			})
			if err != nil {
				return nil, liveWorkouts, err
			}
			workoutID = extractResponseID([]byte(response.Body), "workoutId", "workout_id", "id")
			if workoutID == "" {
				return nil, liveWorkouts, fmt.Errorf("Garmin upload returned HTTP %d without workoutId", response.Status)
			}
			if response.BaseURL == "verified-live-state" {
				uploadStatus = "recovered"
			}
			liveWorkouts = append(liveWorkouts, types.Workout{WorkoutId: workoutID, WorkoutName: item.Draft.Name})
		}
		if err := store.MarkApplied(item.Draft.ID, workoutID, "", ""); err != nil {
			return nil, liveWorkouts, fmt.Errorf("checkpointing uploaded workout: %w", err)
		}
	}

	result := map[string]any{
		"draft_id":      item.Draft.ID,
		"name":          item.Draft.Name,
		"workout_id":    workoutID,
		"upload_status": uploadStatus,
	}
	if item.Schedule == "" {
		return result, liveWorkouts, nil
	}

	target := workoutReconcileTarget{
		Workout: types.Workout{WorkoutId: workoutID, WorkoutName: item.Draft.Name},
		Date:    item.Schedule,
	}
	scheduleID, found, err := findGarminCalendarWorkoutWithMutationSession(session, target)
	if err != nil {
		return nil, liveWorkouts, err
	}
	scheduleStatus := "existing"
	if !found {
		path := "/workout-service/schedule/" + workoutID
		response, err := mutate("POST", path, map[string]string{"date": item.Schedule}, func() (browserPostResponse, bool, error) {
			id, found, err := findGarminCalendarWorkoutWithMutationSession(session, target)
			if err != nil || !found {
				return browserPostResponse{}, found, err
			}
			body, _ := json.Marshal(map[string]string{"workoutScheduleId": id})
			return browserPostResponse{BaseURL: "verified-live-state", Status: 200, Body: string(body)}, true, nil
		})
		if err != nil {
			return nil, liveWorkouts, err
		}
		scheduleID = extractResponseID([]byte(response.Body), "workoutScheduleId", "scheduledWorkoutId", "id")
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				if err := waitForGarminMutationSpacing(ctx, time.Second); err != nil {
					return nil, liveWorkouts, err
				}
			}
			verifiedID, verified, err := findGarminCalendarWorkoutWithMutationSession(session, target)
			if err != nil {
				return nil, liveWorkouts, err
			}
			if verified {
				if scheduleID == "" {
					scheduleID = verifiedID
				}
				found = true
				break
			}
		}
		if !found {
			return nil, liveWorkouts, fmt.Errorf("Garmin accepted the schedule, but %q did not appear on %s", item.Draft.Name, item.Schedule)
		}
		scheduleStatus = "created"
	}
	if err := store.MarkApplied(item.Draft.ID, workoutID, scheduleID, item.Schedule); err != nil {
		return nil, liveWorkouts, fmt.Errorf("checkpointing scheduled workout: %w", err)
	}
	result["schedule_status"] = scheduleStatus
	result["scheduled_date"] = item.Schedule
	result["scheduled_workout_id"] = scheduleID
	return result, liveWorkouts, nil
}

func listGarminWorkoutsWithMutationSession(session *garminBrowserMutationSession, limit int) ([]types.Workout, error) {
	path := fmt.Sprintf("/workout-service/workouts?start=0&limit=%d", limit)
	response, err := session.read(path)
	if err != nil {
		return nil, err
	}
	workouts, err := decodeGarminWorkoutPage([]byte(response.Body))
	if err != nil {
		return nil, fmt.Errorf("decoding Garmin workout list: %w", err)
	}
	if err := validateGarminWorkoutListLimit(len(workouts), limit); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, workout := range workouts {
		if workout.WorkoutId == "" || workout.WorkoutName == "" {
			return nil, fmt.Errorf("Garmin workout list returned an item without workoutId or workoutName")
		}
		if _, duplicate := seen[workout.WorkoutId]; duplicate {
			return nil, fmt.Errorf("Garmin workout list repeated workoutId %s", workout.WorkoutId)
		}
		seen[workout.WorkoutId] = struct{}{}
	}
	return workouts, nil
}

func verifyGarminWorkoutPayloadWithMutationSession(
	session *garminBrowserMutationSession,
	workoutID string,
	expected map[string]any,
) (browserPostResponse, bool, string, error) {
	response, err := session.read("/workout-service/workout/" + workoutID)
	if err != nil {
		return response, false, "", err
	}
	matches, mismatch, err := compareGarminWorkoutPayload(expected, response.Body)
	return response, matches, mismatch, err
}

func findGarminCalendarWorkoutWithMutationSession(session *garminBrowserMutationSession, target workoutReconcileTarget) (string, bool, error) {
	date, err := time.Parse("2006-01-02", target.Date)
	if err != nil {
		return "", false, err
	}
	path := fmt.Sprintf("/calendar-service/year/%d/month/%d", date.Year(), int(date.Month())-1)
	response, err := session.read(path)
	if err != nil {
		return "", false, err
	}
	return findGarminCalendarWorkoutInData([]byte(response.Body), target)
}

func exactGarminWorkoutNameMatches(workouts []types.Workout, name string) []types.Workout {
	var matches []types.Workout
	for _, workout := range workouts {
		if workout.WorkoutName == name {
			matches = append(matches, workout)
		}
	}
	return matches
}

func containsGarminWorkoutID(workouts []types.Workout, id string) bool {
	for _, workout := range workouts {
		if workout.WorkoutId == id {
			return true
		}
	}
	return false
}

func waitForGarminMutationSpacing(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
