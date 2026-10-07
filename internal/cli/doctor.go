// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"time"

	"garmin-connect-workout-cli/internal/garminsession"
	"garmin-connect-workout-cli/internal/workoutdraft"

	"github.com/spf13/cobra"
)

func newDoctorCmd(flags *rootFlags) *cobra.Command {
	var live bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the saved Garmin login, draft history, and (with --live) Garmin access",
		Example: `  garmin-connect-workout-cli doctor
  garmin-connect-workout-cli doctor --live --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			report := map[string]any{"version": version}
			var problems []string
			fail := func(key, msg string) {
				report[key] = msg
				problems = append(problems, key+": "+msg)
			}

			if dir, ready, err := garminsession.BrowserProfileReady(); err != nil {
				fail("browser_profile", err.Error())
			} else if !ready {
				fail("browser_profile", "missing; run auth login-browser")
			} else {
				report["browser_profile"] = dir
			}

			session, path, found, err := garminsession.Load()
			switch {
			case err != nil:
				fail("web_session", err.Error())
			case !found:
				fail("web_session", "missing; run auth login-browser")
			case !session.Active(time.Now()):
				fail("web_session", "expired; run auth login-browser")
			default:
				report["web_session"] = map[string]any{"path": path, "captured_at": session.CapturedAt, "verified_at": session.VerifiedAt}
			}

			if store, err := workoutdraft.NewStore(); err != nil {
				fail("drafts", err.Error())
			} else if drafts, err := store.List(); err != nil {
				fail("drafts", err.Error())
			} else {
				report["drafts"] = map[string]any{"path": store.Path, "count": len(drafts)}
			}

			if err := checkGarminMutationCircuit(time.Now()); err != nil {
				fail("write_circuit", err.Error())
			} else {
				report["write_circuit"] = "closed"
			}

			if live {
				if _, _, err := garminBrowserGetJSON(cmd.Context(), "/workout-service/workout/types"); err != nil {
					fail("garmin_live", err.Error())
				} else {
					report["garmin_live"] = "ok"
				}
			}

			report["ok"] = len(problems) == 0
			human := "All checks passed.\n"
			if len(problems) > 0 {
				human = ""
				for _, p := range problems {
					human += "FAIL " + p + "\n"
				}
			}
			if err := printJSONOrHuman(cmd, flags, report, human); err != nil {
				return err
			}
			if len(problems) > 0 {
				return configErr(fmt.Errorf("%d doctor check(s) failed", len(problems)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&live, "live", false, "Also read Garmin through the saved browser session")
	return cmd
}
