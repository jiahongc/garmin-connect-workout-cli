// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"garmin-connect-workout-cli/internal/workoutdraft"
	"garmin-connect-workout-cli/internal/workoutprefs"

	"github.com/spf13/cobra"
)

func newNovelWorkoutsPlanCmd(flags *rootFlags) *cobra.Command {
	var flagDate string
	var flagName string

	cmd := &cobra.Command{
		Use:         "plan <workout>",
		Short:       "Turn a plain-English running workout into Garmin-compatible workout steps without writing to Garmin.",
		Example:     "  garmin-connect-workout-cli workouts plan \"10 min warmup, 6x800m at 5K pace with 2 min jog, 10 min cooldown\" --date 2026-07-01 --json --agent",
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) == 0 {
				return usageErr(fmt.Errorf("plain-English workout is required"))
			}
			if dryRunOK(flags) {
				fmt.Fprintln(cmd.OutOrStdout(), "would parse workout and save a local draft")
				return nil
			}
			allowInput := !flags.noInput && !flags.agent && !flags.asJSON && isTerminalInput(cmd.InOrStdin())
			prefs, _, found, err := workoutprefs.Load()
			if err != nil {
				return configErr(err)
			}
			if !found && allowInput {
				fmt.Fprintln(cmd.ErrOrStderr(), "No saved workout preferences were found. Let's set the ambiguity rules before planning.")
				prefs, save, err := runWorkoutPreferenceQuestionnaire(cmd.InOrStdin(), cmd.ErrOrStderr())
				if err != nil {
					return usageErr(err)
				}
				if save {
					path, err := workoutprefs.Save(prefs)
					if err != nil {
						return configErr(err)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Workout preferences saved locally: %s\n", path)
				}
			}
			preparedPrompt := workoutdraft.ApplyRecoveryPreferences(args[0], prefs.StrideRecovery, prefs.HillSprintRecovery)
			prompt, err := resolveWorkoutClarifications(
				cmd.InOrStdin(),
				cmd.ErrOrStderr(),
				preparedPrompt,
				allowInput,
			)
			if err != nil {
				return usageErr(err)
			}
			draft, err := workoutdraft.Plan(prompt, flagDate, flagName)
			if err != nil {
				return usageErr(err)
			}
			store, err := workoutdraft.NewStore()
			if err != nil {
				return configErr(err)
			}
			if err := store.Save(draft); err != nil {
				return configErr(fmt.Errorf("saving draft: %w", err))
			}
			out := map[string]any{
				"draft_id":       draft.ID,
				"stored":         true,
				"history_path":   store.Path,
				"workout":        draft.Workout,
				"garmin_payload": draft.GarminPayload,
			}
			if flags.asJSON || flags.agent || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Draft saved: %s\n", draft.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Workout: %s\n", draft.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Steps: %d\n", len(draft.Workout.Steps))
			if draft.Date != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Date: %s\n", draft.Date)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nPreview JSON with:\n  garmin-connect-workout-cli history search %q --json\n", draft.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagDate, "date", "", "Optional schedule date in YYYY-MM-DD format")
	cmd.Flags().StringVar(&flagName, "name", "", "Workout name to save in Garmin Connect")
	return cmd
}

func resolveWorkoutClarifications(in io.Reader, errOut io.Writer, prompt string, allowInput bool) (string, error) {
	reader := bufio.NewReader(in)
	current := strings.TrimSpace(prompt)
	for round := 0; round < 3; round++ {
		questions := workoutdraft.Clarifications(current)
		if len(questions) == 0 {
			return current, nil
		}
		if !allowInput {
			return "", workoutClarificationError(questions)
		}

		fmt.Fprintln(errOut, "Before I create this workout, I need to clarify:")
		for index, question := range questions {
			fmt.Fprintf(errOut, "  %d. %s\n", index+1, question.Question)
		}
		fmt.Fprint(errOut, "Enter the complete workout again with those details included: ")
		revised, err := reader.ReadString('\n')
		if err != nil && revised == "" {
			return "", fmt.Errorf("reading clarified workout: %w", err)
		}
		current = strings.TrimSpace(revised)
		if current == "" {
			return "", fmt.Errorf("clarified workout cannot be empty")
		}
	}
	return "", workoutClarificationError(workoutdraft.Clarifications(current))
}

func workoutClarificationError(questions []workoutdraft.Clarification) error {
	var message strings.Builder
	message.WriteString("workout needs clarification before a draft can be created:")
	for _, question := range questions {
		message.WriteString("\n- ")
		message.WriteString(question.Question)
	}
	message.WriteString("\nrerun workouts plan with a complete workout description")
	return fmt.Errorf("%s", message.String())
}

func isTerminalInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return true
	}
	return info.Mode()&os.ModeCharDevice != 0
}
