# Agent Guide

This repo contains `garmin-connect-workout-cli`, a Go CLI for creating Garmin Connect running workouts from natural language.

## Working Rules

- Keep changes small and directly tied to the requested behavior.
- Do not ask users to paste Garmin credentials into chat.
- Prefer `auth login-browser` for Garmin sign-in and MFA.
- Treat browser profile data and token files as local secrets.
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
- `history list`
- `history search`
- `auth status`
- `doctor`
- `version`

Live Garmin write commands:

- `workouts apply --apply`
- `workouts upload-json`
- `workouts delete`
- `schedule create`
- `schedule delete`

For live Garmin writes, show the user what will be sent and get confirmation first unless the user has already explicitly asked you to perform the write.

## Workout Planning Notes

- If a date is provided, `workouts apply --apply` schedules the workout on that date by default.
- Use `--no-schedule` when the user wants the workout uploaded but not added to the calendar.
- When recovery is missing, the planner uses conservative defaults for common running patterns:
  - Strides: 60 seconds.
  - Hill sprints or full recovery: 90 seconds.
- If the recovery is important and ambiguous, ask the user before applying the workout.
- Keep unstructured coaching notes in notes/description instead of inventing workout steps.

## Browser Auth

`auth login-browser` opens visible Chrome. Subsequent workout writes use the saved Chrome profile headlessly.

If Garmin rejects a write with an auth error, run:

```bash
garmin-connect-workout-cli auth login-browser
```

To debug browser writes visibly:

```bash
GARMIN_CONNECT_BROWSER_HEADLESS=0 garmin-connect-workout-cli workouts apply <draft-id> --apply
```

