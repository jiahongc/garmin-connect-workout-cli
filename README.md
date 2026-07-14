# garmin-connect-workout-cli

A small, unofficial CLI for creating running workouts in Garmin Connect from plain English.

Garmin does not provide a public consumer API for creating workouts. This tool uses the same private Garmin Connect web endpoints the Garmin website uses after you sign in. It may break if Garmin changes those endpoints or blocks automated access.

The main flow is:

1. Sign in to Garmin Connect once in a real Chrome window.
2. Ask the CLI to turn a workout description into a Garmin workout draft.
3. Review the generated payload.
4. Apply it to Garmin Connect. If the draft has a date, the CLI also adds it to the Garmin calendar by default.

The CLI can also list, inspect, update, delete, and schedule existing Garmin workouts. Routine single-workout commands reuse the saved Garmin session automatically. For a multi-workout cleanup, `workouts reconcile` keeps one visible authenticated Chrome session open while the CLI performs and verifies the API operations.

## Install

Requirements:

- Go 1.26+
- Chrome
- A Garmin Connect account

Install from this repo:

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
- The CLI saves a local Garmin web session for later workout uploads and schedule changes.

The saved browser profile and session are local secrets. Do not share them.

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
garmin-connect-workout-cli workouts plan "35min easy + drills + 4x20s strides relaxed" \
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

## Inspect And Update Existing Workouts

List workouts currently stored in Garmin Connect:

```bash
garmin-connect-workout-cli workouts list --limit 20 --json
```

Inspect the exact steps for one workout:

```bash
garmin-connect-workout-cli workouts get 1620262629 --json
```

To correct an existing workout, create and review a new local draft, then replace the Garmin workout in place:

```bash
garmin-connect-workout-cli workouts apply draft_abc123 \
  --replace 1620262629 \
  --apply \
  --json
```

Without `--apply`, this prints the exact `PUT` path and Garmin payload without changing Garmin Connect. A replacement keeps the same workout ID and does not create another calendar schedule by default, so an existing scheduled workout remains linked. Pass `--schedule YYYY-MM-DD` only when you intentionally want to add another schedule entry.

Delete one workout template:

```bash
garmin-connect-workout-cli workouts delete 1620262629 --yes --json
```

Add an existing workout to the Garmin calendar:

```bash
garmin-connect-workout-cli schedule create 1620262629 \
  --date 2026-08-04 \
  --apply \
  --yes \
  --json
```

Both commands are live Garmin writes. `workouts delete` removes the workout template; `schedule create` keeps the template and adds a calendar entry.

## Reconcile A Workout Library

`workouts reconcile` is for replacing a cluttered Garmin workout library with an exact desired set. It can also ensure that selected kept workouts are present on their intended calendar dates. The behavior is based on exact workout names rather than a hard-coded date rule, so the same command works for any training plan.

Run it without `--apply` first. This opens one visible Chrome session, reads the live Garmin library, validates that every kept name matches exactly one workout, and prints the proposed counts without changing Garmin:

```bash
garmin-connect-workout-cli workouts reconcile \
  --keep-name "Tuesday Intervals" \
  --keep-name "Saturday Long Run" \
  --schedule "Tuesday Intervals=2026-08-04" \
  --schedule "Saturday Long Run=2026-08-08" \
  --json
```

Review `delete_count`, then rerun with that exact count, `--apply`, and `--yes`:

```bash
garmin-connect-workout-cli workouts reconcile \
  --keep-name "Tuesday Intervals" \
  --keep-name "Saturday Long Run" \
  --schedule "Tuesday Intervals=2026-08-04" \
  --schedule "Saturday Long Run=2026-08-08" \
  --expect-delete 12 \
  --apply \
  --yes \
  --json
```

Safety behavior:

- Every `--keep-name` must match exactly one live Garmin workout. Missing or duplicate names abort before writes.
- `--apply` requires the exact `--expect-delete` count shown by the dry run.
- Existing matching calendar entries are verified and reused instead of duplicated.
- The command verifies the remaining workout set and requested calendar dates before it exits.
- The default `--list-limit` is 100. If the response reaches that limit, reconciliation aborts; rerun with a higher `--list-limit` so the CLI can prove it saw the complete library.

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
35min easy + drills + 4x20s strides
40min easy by feel at RPE 2-3
30min easy + 6x10s hill sprint with full recovery
10 min warmup, 6x800m at 5K pace with 2 min jog, 10 min cooldown
10 min warmup, 4x1km at 4:30/km with 90 sec jog, 10 min cooldown
2mi easy warmup, 2 sets of (4 min at 10K effort, 60 sec float, 3 min at 5K effort, 60 sec float, 2 min at 3K effort) with 3 min jog between sets, 15 min cooldown
```

Default recovery rules:

- Strides without recovery: 60 seconds easy recovery.
- Hill sprints without a recovery instruction: 90 seconds recovery.
- `full recovery` without a duration: Garmin waits for the Lap button and skips the recovery after the final repetition.
- An explicit duration such as `2 min full recovery` remains a timed recovery.
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
- Treat `workouts apply`, `workouts upload-json`, `schedule create`, `schedule delete`, `workouts delete`, and `workouts reconcile --apply` as live Garmin writes.
- If the user gives a date, expect `workouts apply` to schedule the workout unless `--no-schedule` is used.

## Browser Behavior

Login uses visible Chrome because Garmin sign-in and MFA are interactive.

Workout reads and writes are private Garmin Connect web API calls. The CLI uses a direct API token when available; otherwise it sends requests through the saved signed-in Chrome profile in a headless browser context. This is internal to the CLI and does not open a browser window during normal `workouts list`, `workouts get`, or `workouts apply` commands. To debug that session-backed path visibly:

```bash
GARMIN_CONNECT_BROWSER_HEADLESS=0 garmin-connect-workout-cli workouts apply draft_abc123 --apply
```

`workouts reconcile` intentionally opens one visible Chrome window and keeps that authenticated context alive for the complete read/delete/schedule/verify sequence. The CLI sends API requests from that context; it does not click through the Garmin website UI.

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
