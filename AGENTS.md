# Agent Guide

This repo contains `garmin-connect-workout-cli`, a Go CLI for creating Garmin Connect running workouts from natural language.

## Working Rules

- Keep changes small and directly tied to the requested behavior.
- Do not ask users to paste Garmin credentials into chat.
- Prefer `auth login-browser` for Garmin sign-in and MFA.
- Treat the browser profile and saved web session as local secrets.
- Do not commit or push unless the user explicitly asks.

## Commands

```bash
make test
make build
go test ./...
go build ./cmd/garmin-connect-workout-cli
```

## Safe And Live Commands

Safe local commands:

- `workouts plan`
- `history search` (no query lists all drafts)
- `auth status`
- `doctor` (`doctor --live` also reads Garmin through the saved session)
- `version`

Live Garmin write commands:

- `workouts apply --apply`
- `workouts apply-batch --apply --yes`
- `workouts reconcile --apply --yes`
- `workouts upload-json --apply`
- `workouts delete --apply`
- `schedule create --apply`
- `schedule delete --apply`

Without `--apply`, these commands only print what would be sent.

For live Garmin writes, show the user what will be sent and get confirmation first unless the user has already explicitly asked you to perform the write.

## Workout Planning Notes

- If a date is provided, `workouts apply --apply` schedules the workout on that date by default.
- Use `--no-schedule` when the user wants the workout uploaded but not added to the calendar.
- When recovery for strides or hill sprints is missing, the planner stops with a clarification question unless the user saved a matching preference (`preferences setup`). Ask the user rather than guessing.
- `full recovery` without a duration becomes a Lap-button step.
- Keep unstructured coaching notes in notes/description instead of inventing workout steps. Put them after `Note:` so they are never parsed as steps.

## Browser Auth

`auth login-browser` checks the saved login headlessly first and opens visible Chrome only when sign-in or MFA is needed. Subsequent workout reads and writes use the saved Chrome profile headlessly.

If Garmin rejects a write with an auth error, run:

```bash
garmin-connect-workout-cli auth login-browser
```

To debug browser writes visibly:

```bash
GARMIN_CONNECT_BROWSER_HEADLESS=0 garmin-connect-workout-cli workouts apply <draft-id> --apply
```

