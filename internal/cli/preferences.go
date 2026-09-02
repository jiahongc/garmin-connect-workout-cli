// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"garmin-connect-workout-cli/internal/workoutdraft"
	"garmin-connect-workout-cli/internal/workoutprefs"
	"github.com/spf13/cobra"
)

func newPreferencesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preferences",
		Short: "Manage private local workout-planning preferences",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newPreferencesSetupCmd(flags))
	cmd.AddCommand(newPreferencesShowCmd(flags))
	return cmd
}

func newPreferencesSetupCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Answer a short workout-planning questionnaire",
		Long:  "Asks how omitted stride and hill-sprint recoveries should be handled, then saves the answers locally only with explicit consent.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.noInput || flags.agent || flags.asJSON || !isTerminalInput(cmd.InOrStdin()) {
				return usageErr(fmt.Errorf("preferences setup requires an interactive terminal"))
			}
			prefs, save, err := runWorkoutPreferenceQuestionnaire(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return usageErr(err)
			}
			if !save {
				fmt.Fprintln(cmd.OutOrStdout(), "Preferences were not saved.")
				return nil
			}
			path, err := workoutprefs.Save(prefs)
			if err != nil {
				return configErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Workout preferences saved locally: %s\n", path)
			return nil
		},
	}
}

func newPreferencesShowCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:         "show",
		Short:       "Show saved workout-planning preferences",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"agent:read-only": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			prefs, path, found, err := workoutprefs.Load()
			if err != nil {
				return configErr(err)
			}
			result := map[string]any{
				"configured": found,
				"path":       path,
				"preferences": map[string]string{
					"stride_recovery":      prefs.StrideRecovery,
					"hill_sprint_recovery": prefs.HillSprintRecovery,
				},
			}
			if flags.asJSON || flags.agent || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), result, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configured: %t\n", found)
			fmt.Fprintf(cmd.OutOrStdout(), "Stride recovery: %s\n", prefs.StrideRecovery)
			fmt.Fprintf(cmd.OutOrStdout(), "Hill-sprint recovery: %s\n", prefs.HillSprintRecovery)
			fmt.Fprintf(cmd.OutOrStdout(), "Local file: %s\n", path)
			return nil
		},
	}
}

func runWorkoutPreferenceQuestionnaire(in io.Reader, out io.Writer) (workoutprefs.Preferences, bool, error) {
	reader := bufio.NewReader(in)
	fmt.Fprintln(out, "Workout planning preferences (explicit workout text always wins).")

	stride, err := askRecoveryPreference(reader, out, "Stride recovery when the workout omits it:", []string{
		workoutprefs.Ask,
		workoutprefs.TripleStrideDuration,
		"40 sec easy jog",
		"60 sec easy jog",
		"full recovery",
	})
	if err != nil {
		return workoutprefs.Preferences{}, false, err
	}
	hill, err := askRecoveryPreference(reader, out, "Hill-sprint recovery when the workout omits it:", []string{
		workoutprefs.Ask,
		"walk down the hill",
		"60 sec easy jog",
		"90 sec easy jog",
	})
	if err != nil {
		return workoutprefs.Preferences{}, false, err
	}

	fmt.Fprint(out, "Save these preferences locally for future workouts? [y/N]: ")
	answer, err := readQuestionnaireLine(reader)
	if err != nil {
		return workoutprefs.Preferences{}, false, fmt.Errorf("reading save choice: %w", err)
	}
	save := strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
	return workoutprefs.Preferences{
		Version:            workoutprefs.CurrentVersion,
		StrideRecovery:     stride,
		HillSprintRecovery: hill,
	}, save, nil
}

func askRecoveryPreference(reader *bufio.Reader, out io.Writer, label string, choices []string) (string, error) {
	fmt.Fprintln(out, label)
	for index, choice := range choices {
		description := choice
		if choice == workoutprefs.Ask {
			description = "Always ask (recommended)"
		}
		fmt.Fprintf(out, "  %d. %s\n", index+1, description)
	}
	customIndex := len(choices) + 1
	fmt.Fprintf(out, "  %d. Custom recovery\n", customIndex)
	fmt.Fprintf(out, "Choose 1-%d: ", customIndex)
	raw, err := readQuestionnaireLine(reader)
	if err != nil {
		return "", fmt.Errorf("reading recovery choice: %w", err)
	}
	choice, err := strconv.Atoi(raw)
	if err != nil || choice < 1 || choice > customIndex {
		return "", fmt.Errorf("recovery choice must be a number from 1 to %d", customIndex)
	}
	if choice <= len(choices) {
		return choices[choice-1], nil
	}

	fmt.Fprint(out, "Enter an exact time, distance, or manual recovery (for example 45 sec easy jog, 200 m jog, or full recovery): ")
	custom, err := readQuestionnaireLine(reader)
	if err != nil {
		return "", fmt.Errorf("reading custom recovery: %w", err)
	}
	if !validRecoveryPreference(custom) {
		return "", fmt.Errorf("custom recovery must specify a time, distance, or manual recovery such as full recovery or walk down")
	}
	return custom, nil
}

func validRecoveryPreference(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, workoutprefs.Ask) {
		return false
	}
	sample := "4x20s relaxed strides"
	resolved := workoutdraft.ApplyRecoveryPreferences(sample, value, workoutprefs.Ask)
	if resolved == sample {
		return false
	}
	for _, clarification := range workoutdraft.Clarifications(resolved) {
		if clarification.Code == "repeat_recovery" {
			return false
		}
	}
	return true
}

func readQuestionnaireLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
