# garmin-connect-workout-cli

A small CLI for creating running workouts in Garmin Connect from plain English.

The main flow is:

1. Sign in to Garmin Connect once in a real Chrome window.
2. Ask the CLI to turn a workout description into a Garmin workout draft.
3. Review the generated payload.
4. Apply it to Garmin Connect. If the draft has a date, the CLI also adds it to the Garmin calendar by default.

## Install

Requirements:

- Go 1.26+
- Chrome
- A Garmin Connect account

From this repo:

```bash
go install ./cmd/garmin-connect-workout-cli
```

Or build a local binary:

```bash
make build
./bin/garmin-connect-workout-cli version
```

## Login

Use browser login. This keeps your Garmin password out of the terminal and lets Garmin handle MFA normally.

```bash
garmin-connect-workout-cli auth login-browser
```

What happens:

- The CLI opens a visible Chrome window.
- You sign in to Garmin Connect and complete MFA there.
- The CLI verifies that the browser profile can read Garmin workouts.
- The CLI saves a local Garmin web session for later workout writes.

Check local auth state:

```bash
garmin-connect-workout-cli auth status
```

Clear the saved local browser profile:

```bash
garmin-connect-workout-cli auth logout
```

## Create And Schedule A Workout

Plan a workout first. Planning is local only and does not write to Garmin.

```bash
garmin-connect-workout-cli workouts plan "35min E + Drills + 4x20s strides 全程放松，不追配速" \
  --date 2026-06-23 \
  --json
```

Use the returned `draft_id`, review the payload, then apply it:

```bash
garmin-connect-workout-cli workouts apply draft_abc123 --apply --json
```

If the draft has `--date`, `workouts apply` schedules it on that date automatically. To upload without adding it to the Garmin calendar:

```bash
garmin-connect-workout-cli workouts apply draft_abc123 --apply --no-schedule
```

To override the draft date:

```bash
garmin-connect-workout-cli workouts apply draft_abc123 --apply --schedule 2026-06-25
```

## Workout Naming

If you do not pass `--name`, the CLI tries to create a clear Garmin title from the date and main workout.

Example:

```bash
garmin-connect-workout-cli workouts plan "35min E + Drills + 4x20s strides" --date 2026-06-23
```

Default title:

```text
June 23: 35E + Drills + 4x20s strides
```

Warmups and cooldowns are not emphasized in inferred titles. Extra text that is not a workout step is retained as notes instead of being silently discarded.

You can always set the exact title:

```bash
garmin-connect-workout-cli workouts plan "10 min warmup, 6x800m at 5K pace with 2 min jog, 10 min cooldown" \
  --date 2026-07-01 \
  --name "July 1: 6x800m at 5K"
```

## Supported Workout Phrases

Good inputs:

```text
35min E + Drills + 4x20s strides
40min E 炎热时按 RPE 2-3
30min E + 6x10" Hill Sprint 每次全恢复
10 min warmup, 6x800m at 5K pace with 2 min jog, 10 min cooldown
10 min warmup, 4x1km at 4:30/km with 90 sec jog, 10 min cooldown
```

Default recovery rules:

- Strides without recovery: 60 seconds easy recovery.
- Hill sprints or “full recovery” without a time: 90 seconds recovery.
- If recovery matters, be explicit: `with 2 min jog`, `with 90 sec walk`, `with 400m jog`.

## Agent Usage

This CLI is designed to be called by coding agents such as Codex, Hermes Agent, Claude Code, Cursor, or any tool that can run shell commands.

Use JSON output for agent workflows:

```bash
garmin-connect-workout-cli workouts plan "30min E + 6x10s hill sprint full recovery" \
  --date 2026-06-25 \
  --json --agent
```

Then apply only after showing the user the draft and getting confirmation:

```bash
garmin-connect-workout-cli workouts apply draft_abc123 --apply --json --agent
```

Agent safety rules:

- Do not ask the user to paste a Garmin password into chat.
- Use `auth login-browser` for login and MFA.
- Treat `workouts plan` as safe: it only writes a local draft.
- Treat `workouts apply`, `workouts upload-json`, `schedule create`, `schedule delete`, and `workouts delete` as live Garmin writes.
- If the user gives a date, expect `workouts apply` to schedule the workout unless `--no-schedule` is used.

## Browser Behavior

Login uses visible Chrome because Garmin sign-in and MFA are interactive.

Workout writes use the saved Garmin web session by default. If no saved token or web session is available, the CLI falls back to the browser profile. To debug that fallback visibly:

```bash
GARMIN_CONNECT_BROWSER_HEADLESS=0 garmin-connect-workout-cli workouts apply draft_abc123 --apply
```

The login persists in the local Chrome profile until Garmin expires or invalidates it. If writes start returning an auth error, run:

```bash
garmin-connect-workout-cli auth login-browser
```

## Development

```bash
make test
make build
```

Useful local files:

- Draft history: `~/Library/Application Support/garmin-connect-workout-cli/workout-drafts.json` on macOS.
- Browser profile: `~/Library/Application Support/garmin-connect-workout-cli/browser-profile` on macOS.
