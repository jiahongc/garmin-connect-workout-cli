// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"strconv"
	"time"

	"garmin-connect-workout-cli/internal/workoutdraft"
	"github.com/spf13/cobra"
)

func newNovelPlanRaceBackwardCmd(flags *rootFlags) *cobra.Command {
	var flagRaceDate string
	var flagWeeks string
	var flagGoal string

	cmd := &cobra.Command{
		Use:         "race-backward",
		Short:       "Build and schedule a training block backwards from a race date.",
		Example:     "  garmin-connect-workout-cli plan race-backward --race-date 2026-10-11 --weeks 8 --goal \"sub-45 10K\" --json",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if !isISODate(flagRaceDate) || flagRaceDate == "" {
				return usageErr(fmt.Errorf("--race-date must use YYYY-MM-DD"))
			}
			weeks, err := strconv.Atoi(flagWeeks)
			if err != nil || weeks < 1 {
				return usageErr(fmt.Errorf("--weeks must be a positive integer"))
			}
			if dryRunOK(flags) {
				fmt.Fprintln(cmd.OutOrStdout(), "would create local race-backward workout drafts")
				return nil
			}
			raceDate, _ := time.Parse("2006-01-02", flagRaceDate)
			store, err := workoutdraft.NewStore()
			if err != nil {
				return configErr(err)
			}
			var drafts []workoutdraft.Draft
			for week := weeks; week >= 1; week-- {
				workoutDate := raceDate.AddDate(0, 0, -7*week+2).Format("2006-01-02")
				prompt := raceWeekPrompt(weeks-week+1, weeks, flagGoal)
				name := fmt.Sprintf("Race build W%02d", weeks-week+1)
				draft, err := workoutdraft.Plan(prompt, workoutDate, name)
				if err != nil {
					return err
				}
				if err := store.Save(draft); err != nil {
					return configErr(err)
				}
				drafts = append(drafts, draft)
			}
			out := map[string]any{
				"race_date":    flagRaceDate,
				"goal":         flagGoal,
				"weeks":        weeks,
				"history_path": store.Path,
				"drafts":       drafts,
				"next":         "review each draft, then run workouts apply <draft-id> --schedule <date> --apply",
			}
			if flags.asJSON || flags.agent || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %d local race-backward drafts.\n", len(drafts))
			for _, draft := range drafts {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", draft.ID, draft.Date, draft.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRaceDate, "race-date", "", "Race date in YYYY-MM-DD format")
	cmd.Flags().StringVar(&flagWeeks, "weeks", "8", "Number of weeks to build backwards")
	cmd.Flags().StringVar(&flagGoal, "goal", "", "Race goal, used in draft names and prompts")
	return cmd
}

func raceWeekPrompt(week, total int, goal string) string {
	switch {
	case week >= total:
		return "10 min warmup, 3x400m at 5K pace with 2 min jog, 10 min cooldown"
	case week > total-3:
		return "15 min warmup, 4x800m at 10K pace with 2 min jog, 10 min cooldown"
	case week%2 == 0:
		return "15 min warmup, 5x1km at 10K pace with 400m jog, 10 min cooldown"
	default:
		if goal != "" {
			return "15 min warmup, 20 min tempo at " + goal + " effort, 10 min cooldown"
		}
		return "15 min warmup, 20 min tempo, 10 min cooldown"
	}
}
