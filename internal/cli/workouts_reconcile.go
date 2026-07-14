// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"garmin-connect-workout-cli/internal/garminsession"
	"garmin-connect-workout-cli/internal/types"
	"github.com/spf13/cobra"
)

const defaultGarminWorkoutReconcileLimit = 100

type workoutReconcileSchedule struct {
	Name string
	Date string
}

type workoutReconcileTarget struct {
	Workout types.Workout
	Date    string
}

type workoutReconcilePlan struct {
	Keep     []types.Workout
	Delete   []types.Workout
	Schedule []workoutReconcileTarget
}

func newWorkoutsReconcileCmd(flags *rootFlags) *cobra.Command {
	var keepNames []string
	var scheduleValues []string
	var expectDelete int
	var listLimit int
	var apply bool
	var loginTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Keep an exact workout set and schedule selected workouts in one browser session",
		Example: strings.Join([]string{
			"  garmin-connect-workout-cli workouts reconcile --keep-name 'Tue 7/14: 5x1K + 200s'",
			"  garmin-connect-workout-cli workouts reconcile --keep-name 'Workout A' --schedule 'Workout A=2026-07-16' --expect-delete 3 --apply --yes",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(keepNames) == 0 {
				return usageErr(fmt.Errorf("at least one --keep-name is required"))
			}
			if expectDelete < -1 {
				return usageErr(fmt.Errorf("--expect-delete cannot be less than -1"))
			}
			if listLimit <= 0 {
				return usageErr(fmt.Errorf("--list-limit must be positive"))
			}
			if loginTimeout <= 0 {
				return usageErr(fmt.Errorf("--login-timeout must be positive"))
			}
			if apply && !flags.yes {
				return usageErr(fmt.Errorf("--apply requires --yes because reconcile deletes every workout not explicitly kept"))
			}
			if apply && expectDelete < 0 {
				return usageErr(fmt.Errorf("--apply requires --expect-delete with the exact count shown by a dry run"))
			}

			schedules, err := parseWorkoutReconcileSchedules(scheduleValues)
			if err != nil {
				return usageErr(err)
			}
			profileDir, err := garminsession.BrowserProfileDir()
			if err != nil {
				return configErr(err)
			}
			if err := os.MkdirAll(profileDir, 0o700); err != nil {
				return configErr(fmt.Errorf("creating browser profile dir: %w", err))
			}

			result := map[string]any{"applied": apply}
			fmt.Fprintln(cmd.ErrOrStderr(), "Opening one Garmin browser session for workout reconciliation.")
			fmt.Fprintln(cmd.ErrOrStderr(), "If Garmin asks, sign in and complete MFA in the visible Chrome window.")
			_, err = verifyGarminBrowserProfileWithAction(cmd.Context(), profileDir, loginTimeout, func(browserCtx context.Context) error {
				workouts, err := listGarminWorkoutsForReconcile(browserCtx, listLimit)
				if err != nil {
					return err
				}
				plan, err := buildWorkoutReconcilePlan(workouts, keepNames, schedules, expectDelete)
				if err != nil {
					return err
				}

				result["found"] = len(workouts)
				result["keep_count"] = len(plan.Keep)
				result["delete_count"] = len(plan.Delete)
				result["schedule_count"] = len(plan.Schedule)
				result["kept"] = workoutNames(plan.Keep)
				result["scheduled"] = scheduleSummary(plan.Schedule)
				fmt.Fprintf(cmd.ErrOrStderr(), "Plan: found %d, keep %d, delete %d, schedule %d.\n", len(workouts), len(plan.Keep), len(plan.Delete), len(plan.Schedule))
				for _, workout := range plan.Keep {
					fmt.Fprintf(cmd.ErrOrStderr(), "  KEEP %s (%s)\n", workout.WorkoutName, workout.WorkoutId)
				}
				for _, target := range plan.Schedule {
					fmt.Fprintf(cmd.ErrOrStderr(), "  SCHEDULE %s on %s\n", target.Workout.WorkoutName, target.Date)
				}
				if !apply {
					return nil
				}

				for index, workout := range plan.Delete {
					path := "/workout-service/workout/" + workout.WorkoutId
					if _, err := requireGarminBrowserResponse(browserCtx, "DELETE", path, nil); err != nil {
						return fmt.Errorf("deleting workout %q (%s): %w", workout.WorkoutName, workout.WorkoutId, err)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Deleted %d/%d: %s\n", index+1, len(plan.Delete), workout.WorkoutName)
					if index+1 < len(plan.Delete) || len(plan.Schedule) > 0 {
						if err := waitForGarminMutation(browserCtx); err != nil {
							return err
						}
					}
				}

				verifiedSchedules := make([]map[string]string, 0, len(plan.Schedule))
				for index, target := range plan.Schedule {
					scheduleID, found, err := findGarminCalendarWorkout(browserCtx, target)
					if err != nil {
						return err
					}
					status := "existing"
					if !found {
						path := "/workout-service/schedule/" + target.Workout.WorkoutId
						response, err := requireGarminBrowserResponse(browserCtx, "POST", path, map[string]any{"date": target.Date})
						if err != nil {
							return fmt.Errorf("scheduling workout %q on %s: %w", target.Workout.WorkoutName, target.Date, err)
						}
						scheduleID = extractResponseID([]byte(response.Body), "workoutScheduleId", "scheduledWorkoutId", "scheduled_workout_id", "id")
						for attempt := 0; attempt < 3; attempt++ {
							if attempt > 0 {
								if err := waitForGarminMutation(browserCtx); err != nil {
									return err
								}
							}
							calendarID, calendarFound, err := findGarminCalendarWorkout(browserCtx, target)
							if err != nil {
								return err
							}
							if calendarFound {
								if scheduleID == "" {
									scheduleID = calendarID
								}
								found = true
								break
							}
						}
						if !found {
							return apiErr(fmt.Errorf("Garmin accepted schedule for %q on %s but it did not appear on the calendar", target.Workout.WorkoutName, target.Date))
						}
						status = "created"
					}
					verifiedSchedules = append(verifiedSchedules, map[string]string{
						"date":                 target.Date,
						"name":                 target.Workout.WorkoutName,
						"scheduled_workout_id": scheduleID,
						"status":               status,
					})
					fmt.Fprintf(cmd.ErrOrStderr(), "Schedule verified %d/%d: %s on %s (%s)\n", index+1, len(plan.Schedule), target.Workout.WorkoutName, target.Date, status)
					if index+1 < len(plan.Schedule) {
						if err := waitForGarminMutation(browserCtx); err != nil {
							return err
						}
					}
				}

				remaining, err := listGarminWorkoutsForReconcile(browserCtx, listLimit)
				if err != nil {
					return fmt.Errorf("verifying remaining workouts: %w", err)
				}
				if _, err := buildWorkoutReconcilePlan(remaining, keepNames, nil, 0); err != nil {
					return fmt.Errorf("verifying reconciled workout library: %w", err)
				}
				result["verified"] = true
				result["remaining_count"] = len(remaining)
				result["verified_schedules"] = verifiedSchedules
				return nil
			})
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printJSONFiltered(cmd.OutOrStdout(), result, flags)
		},
	}

	cmd.Flags().StringArrayVar(&keepNames, "keep-name", nil, "Exact workout name to retain (repeat for each workout)")
	cmd.Flags().StringArrayVar(&scheduleValues, "schedule", nil, "Schedule in exact-name=YYYY-MM-DD form (repeatable)")
	cmd.Flags().IntVar(&expectDelete, "expect-delete", -1, "Exact delete count required with --apply; omit for a dry-run preview")
	cmd.Flags().IntVar(&listLimit, "list-limit", defaultGarminWorkoutReconcileLimit, "Maximum workouts to inspect; reconciliation aborts if this limit is reached")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply deletes and schedules after validating the full plan")
	cmd.Flags().DurationVar(&loginTimeout, "login-timeout", 8*time.Minute, "Maximum time for login and reconciliation")
	return cmd
}

func parseWorkoutReconcileSchedules(values []string) ([]workoutReconcileSchedule, error) {
	schedules := make([]workoutReconcileSchedule, 0, len(values))
	for _, value := range values {
		separator := strings.LastIndex(value, "=")
		if separator < 0 {
			return nil, fmt.Errorf("invalid --schedule %q; want exact-name=YYYY-MM-DD", value)
		}
		name := value[:separator]
		date := value[separator+1:]
		name = strings.TrimSpace(name)
		date = strings.TrimSpace(date)
		if name == "" || date == "" {
			return nil, fmt.Errorf("invalid --schedule %q; want exact-name=YYYY-MM-DD", value)
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return nil, fmt.Errorf("invalid schedule date %q: %w", date, err)
		}
		schedules = append(schedules, workoutReconcileSchedule{Name: name, Date: date})
	}
	return schedules, nil
}

func buildWorkoutReconcilePlan(workouts []types.Workout, keepNames []string, schedules []workoutReconcileSchedule, expectDelete int) (workoutReconcilePlan, error) {
	byName := map[string][]types.Workout{}
	for _, workout := range workouts {
		byName[workout.WorkoutName] = append(byName[workout.WorkoutName], workout)
	}
	keepSet := map[string]struct{}{}
	plan := workoutReconcilePlan{}
	for _, name := range keepNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return workoutReconcilePlan{}, fmt.Errorf("--keep-name cannot be empty")
		}
		if _, duplicate := keepSet[name]; duplicate {
			return workoutReconcilePlan{}, fmt.Errorf("duplicate --keep-name %q", name)
		}
		matches := byName[name]
		if len(matches) != 1 {
			return workoutReconcilePlan{}, fmt.Errorf("keep workout %q matched %d live workouts; expected exactly 1", name, len(matches))
		}
		keepSet[name] = struct{}{}
		plan.Keep = append(plan.Keep, matches[0])
	}
	for _, workout := range workouts {
		if _, keep := keepSet[workout.WorkoutName]; !keep {
			plan.Delete = append(plan.Delete, workout)
		}
	}
	if expectDelete >= 0 && len(plan.Delete) != expectDelete {
		return workoutReconcilePlan{}, fmt.Errorf("delete count is %d; expected exactly %d", len(plan.Delete), expectDelete)
	}
	for _, schedule := range schedules {
		if _, keep := keepSet[schedule.Name]; !keep {
			return workoutReconcilePlan{}, fmt.Errorf("scheduled workout %q is not in --keep-name", schedule.Name)
		}
		plan.Schedule = append(plan.Schedule, workoutReconcileTarget{Workout: byName[schedule.Name][0], Date: schedule.Date})
	}
	sort.Slice(plan.Keep, func(i, j int) bool { return plan.Keep[i].WorkoutName < plan.Keep[j].WorkoutName })
	sort.Slice(plan.Delete, func(i, j int) bool { return plan.Delete[i].WorkoutName < plan.Delete[j].WorkoutName })
	return plan, nil
}

func listGarminWorkoutsForReconcile(ctx context.Context, limit int) ([]types.Workout, error) {
	path := fmt.Sprintf("/workout-service/workouts?start=0&limit=%d", limit)
	response, err := requireGarminBrowserResponse(ctx, "GET", path, nil)
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

func validateGarminWorkoutListLimit(count int, limit int) error {
	if count >= limit {
		return fmt.Errorf("Garmin returned %d workouts at --list-limit %d; cannot prove the library is complete, so rerun with a higher --list-limit", count, limit)
	}
	return nil
}

func decodeGarminWorkoutPage(data []byte) ([]types.Workout, error) {
	payload := json.RawMessage(data)
	if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		var wrapped map[string]json.RawMessage
		if err := json.Unmarshal(data, &wrapped); err != nil {
			return nil, err
		}
		if value, ok := wrapped["data"]; ok {
			payload = value
		} else if value, ok := wrapped["workouts"]; ok {
			payload = value
		} else {
			return nil, fmt.Errorf("response did not contain a workout array")
		}
	}
	var raw []struct {
		WorkoutID   json.RawMessage `json:"workoutId"`
		WorkoutName string          `json:"workoutName"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	workouts := make([]types.Workout, 0, len(raw))
	for _, item := range raw {
		workoutID, err := decodeGarminWorkoutID(item.WorkoutID)
		if err != nil {
			return nil, err
		}
		workouts = append(workouts, types.Workout{WorkoutId: workoutID, WorkoutName: item.WorkoutName})
	}
	return workouts, nil
}

func decodeGarminWorkoutID(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, `"`) {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		if value == "" {
			return "", fmt.Errorf("Garmin workout response contained an empty workoutId")
		}
		return value, nil
	}
	if _, err := strconv.ParseUint(trimmed, 10, 64); err != nil {
		return "", fmt.Errorf("invalid Garmin workoutId %q: %w", trimmed, err)
	}
	return trimmed, nil
}

func requireGarminBrowserResponse(ctx context.Context, method string, path string, body any) (browserPostResponse, error) {
	response, err := garminBrowserRequestFromPage(ctx, method, path, body)
	if err != nil {
		return browserPostResponse{}, err
	}
	if response.Status < 200 || response.Status >= 300 {
		return response, garminBrowserHTTPError(method, path, response)
	}
	if bodyLooksLikeHTML(response.Body) {
		return response, authErr(fmt.Errorf("Garmin browser %s %s returned HTML instead of API JSON", method, path))
	}
	return response, nil
}

func findGarminCalendarWorkout(ctx context.Context, target workoutReconcileTarget) (string, bool, error) {
	date, err := time.Parse("2006-01-02", target.Date)
	if err != nil {
		return "", false, err
	}
	path := fmt.Sprintf("/calendar-service/year/%d/month/%d", date.Year(), int(date.Month())-1)
	response, err := requireGarminBrowserResponse(ctx, "GET", path, nil)
	if err != nil {
		return "", false, fmt.Errorf("checking Garmin calendar for %s: %w", target.Date, err)
	}
	return findGarminCalendarWorkoutInData([]byte(response.Body), target)
}

func findGarminCalendarWorkoutInData(data []byte, target workoutReconcileTarget) (string, bool, error) {
	var calendar struct {
		CalendarItems []struct {
			ID        json.RawMessage `json:"id"`
			ItemType  string          `json:"itemType"`
			Title     *string         `json:"title"`
			Date      string          `json:"date"`
			WorkoutID json.RawMessage `json:"workoutId"`
		} `json:"calendarItems"`
	}
	if err := json.Unmarshal(data, &calendar); err != nil {
		return "", false, err
	}
	for _, item := range calendar.CalendarItems {
		if item.Date != target.Date || !strings.Contains(strings.ToLower(item.ItemType), "workout") {
			continue
		}
		workoutID := ""
		if raw := strings.TrimSpace(string(item.WorkoutID)); raw != "" && raw != "null" {
			decoded, err := decodeGarminWorkoutID(item.WorkoutID)
			if err != nil {
				return "", false, err
			}
			workoutID = decoded
		}
		titleMatches := item.Title != nil && *item.Title == target.Workout.WorkoutName
		if workoutID != target.Workout.WorkoutId && !titleMatches {
			continue
		}
		scheduleID, err := decodeGarminWorkoutID(item.ID)
		if err != nil {
			return "", false, err
		}
		return scheduleID, true, nil
	}
	return "", false, nil
}

func waitForGarminMutation(ctx context.Context) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func workoutNames(workouts []types.Workout) []string {
	names := make([]string, 0, len(workouts))
	for _, workout := range workouts {
		names = append(names, workout.WorkoutName)
	}
	return names
}

func scheduleSummary(targets []workoutReconcileTarget) []map[string]string {
	items := make([]map[string]string, 0, len(targets))
	for _, target := range targets {
		items = append(items, map[string]string{"name": target.Workout.WorkoutName, "date": target.Date})
	}
	return items
}
