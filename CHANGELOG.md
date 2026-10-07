# Changelog

Notable changes will be documented here.

## Unreleased

- Standalone Garmin Connect workout CLI.
- Browser-session login (`auth login-browser`) with headless reuse for reads and writes.
- Recovery preferences (`preferences setup`) and clarification questions for ambiguous workouts.
- `workouts apply-batch` and `workouts reconcile` with payload verification and a persistent 427/429 circuit breaker.
- `workouts delete` and `schedule delete` only preview unless `--apply` is passed.
- `schedule get` and `workouts types` read through the browser session instead of returning Garmin's HTML shell.
- `doctor` checks the saved browser session; `doctor --live` verifies Garmin access.
- Planner: `GMP+5-10sec` stays part of the pace target, text after `Note:` stays in notes, the first distance or duration in a step defines it, and recovery jogs no longer leak into titles.
- `GARMIN_CONNECT_BROWSER_HEADLESS=0` now also shows the browser for `workouts apply` and `apply-batch`.
- Removed: password and token login (`auth login`, `auth set-token`, `GARMIN_CONNECT_ACCESS_TOKEN`); all Garmin access uses the saved browser session.
- Removed generic scaffold commands that did not apply to Garmin workouts: `sync`, `search`, `workflow`, `import`, `feedback`, `profile`, `which`, `agent-context`, `plan race-backward`.
- Removed global flags `--config`, `--csv`, `--plain`, `--quiet`, `--deliver`, `--profile`, `--data-source`, `--max-age`, `--no-cache`, `--rate-limit`, `--timeout`, `--allow-partial-failure`, `--no-color`, `--human-friendly`. Output is JSON.
- Removed the local SQLite cache and its dependencies.
