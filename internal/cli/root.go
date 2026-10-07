// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"

	"garmin-connect-workout-cli/internal/cliutil"

	"github.com/spf13/cobra"
)

type rootFlags struct {
	asJSON        bool
	compact       bool
	dryRun        bool
	noInput       bool
	idempotent    bool
	ignoreMissing bool
	yes           bool
	agent         bool
	selectFields  string
	homePath      string
}

// RootCmd returns the Cobra command tree without executing it.
func RootCmd() *cobra.Command {
	var flags rootFlags
	return newRootCmd(&flags)
}

// Execute runs the CLI in non-interactive mode: never prompts, all values via flags or stdin.
func Execute() error {
	var flags rootFlags
	err := newRootCmd(&flags).Execute()
	if err != nil && isCobraUsageError(err) {
		// Cobra/pflag usage errors originate before any RunE, so wrap them
		// here to keep the conventional exit code 2.
		return usageErr(err)
	}
	return err
}

// isCobraUsageError reports whether err matches one of Cobra/pflag's
// pre-RunE usage-error shapes. Detection is by message prefix to match
// the same approach the unknown-flag hint path uses above; neither
// Cobra nor pflag exports typed sentinels for these.
//
// Patterns are anchored to the literal punctuation Cobra and pflag
// emit so an application's own RunE error that happens to contain the
// substring "required flag" or "invalid argument" doesn't get
// misclassified as a usage error.
//
// Patterns covered (Cobra v1.x + pflag v1.x as of 2026-05):
//   - "unknown flag: --foo"                            (pflag)
//   - "unknown shorthand flag: 'x' in -x"              (pflag)
//   - "unknown command \"foo\" for ..."                (Cobra)
//   - "required flag \"foo\" not set"                  (Cobra, single missing)
//   - "required flag(s) \"foo\" not set"               (Cobra, multiple missing)
//   - "flag needs an argument: --foo"                  (pflag, missing value)
//   - "invalid argument \"x\" for \"--y\" flag: ..."   (pflag, parse failure)
//
// Cobra emits the singular form ("required flag") when exactly one
// MarkFlagRequired flag is missing, and the plural form ("required
// flag(s)") only when multiple are missing on the same command. Both
// shapes must be anchored to avoid matching app-level errors that
// happen to mention "required flag" as prose; the trailing space + quote
// (`required flag "`) is the literal punctuation cobra emits.
//
// Returns false for nil err.
func isCobraUsageError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand flag") ||
		strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, `required flag "`) ||
		strings.HasPrefix(msg, `required flag(s) "`) ||
		strings.HasPrefix(msg, "flag needs an argument:") ||
		strings.HasPrefix(msg, `invalid argument "`)
}

func newRootCmd(flags *rootFlags) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "garmin-connect-workout-cli",
		Short: "Create Garmin-ready running workouts from plain English, preview them, then upload and schedule only when approved",
		Long: `Create Garmin-ready running workouts from plain English, preview them, then upload and schedule only when approved.

Highlights:
  • workouts plan   Turn a plain-English running workout into Garmin-compatible workout steps without writing to Garmin.
  • workouts apply   Upload or update a saved draft only after previewing the exact Garmin payload.
  • history search   Find prior authored workouts by prompt, pace target, interval shape, date, or Garmin ID.
  • workouts sync-check   Check local history for whether a draft was uploaded and scheduled on its date.

Agent mode: add --agent to any command for JSON output + non-interactive mode.
Sign in: run 'garmin-connect-workout-cli auth login-browser'; check it with 'auth status'.
See README.md for recipes.`,
		SilenceUsage: true,
		Version:      version,
	}
	rootCmd.SetVersionTemplate("garmin-connect-workout-cli {{ .Version }}\n")

	rootCmd.PersistentFlags().BoolVar(&flags.asJSON, "json", false, "Output as JSON")
	rootCmd.PersistentFlags().BoolVar(&flags.compact, "compact", false, "Return only key fields (id, name, status, timestamps) for minimal token usage")
	rootCmd.PersistentFlags().StringVar(&flags.selectFields, "select", "", "Comma-separated fields to include in output (e.g. --select id,name,status)")
	rootCmd.PersistentFlags().StringVar(&flags.homePath, "home", "", "Root directory for config, data, state, and cache files")
	rootCmd.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false, "Show request without sending")
	rootCmd.PersistentFlags().BoolVar(&flags.noInput, "no-input", false, "Disable all interactive prompts (for CI/agents)")
	rootCmd.PersistentFlags().BoolVar(&flags.idempotent, "idempotent", false, "Treat already-existing create results as a successful no-op")
	rootCmd.PersistentFlags().BoolVar(&flags.ignoreMissing, "ignore-missing", false, "Treat missing delete targets as a successful no-op")
	rootCmd.PersistentFlags().BoolVar(&flags.yes, "yes", false, "Skip confirmation prompts (for agents and scripts)")
	rootCmd.PersistentFlags().BoolVar(&flags.agent, "agent", false, "Set all agent-friendly defaults (--json --compact --no-input --yes)")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if _, err := cliutil.SetHomeOverride(flags.homePath); err != nil {
			return err
		}
		if flags.agent {
			if !cmd.Flags().Changed("json") {
				flags.asJSON = true
			}
			if !cmd.Flags().Changed("compact") {
				flags.compact = true
			}
			if !cmd.Flags().Changed("no-input") {
				flags.noInput = true
			}
			if !cmd.Flags().Changed("yes") {
				flags.yes = true
			}
		}
		return nil
	}
	rootCmd.AddCommand(newScheduleCmd(flags))
	rootCmd.AddCommand(newWorkoutsCmd(flags))
	rootCmd.AddCommand(newDoctorCmd(flags))
	rootCmd.AddCommand(newAuthCmd(flags))
	rootCmd.AddCommand(newPreferencesCmd(flags))
	rootCmd.AddCommand(newNovelHistoryCmd(flags))
	rootCmd.AddCommand(newVersionCmd())

	return rootCmd
}

func ExitCode(err error) int {
	var codeErr *cliError
	if As(err, &codeErr) {
		return codeErr.code
	}
	return 1
}
