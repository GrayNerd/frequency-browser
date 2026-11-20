# Settings Persistence Handoff

## Status
Persistent settings + tabbed Settings window implemented and building. Runtime verification pending.

## Done
- settings.config in the user data dir (go-app-paths); auto-created with defaults on first run.
- Settings window: Appearance/Radio tabs; theme (light default), Save Window Size/Position, Save Now; rigctld address.
- Light/dark themes via theme.css; live switch via the themeChanged event.
- Main window geometry restored while hidden before show; saved on exit when enabled.
- go build, go test -short, and frontend npm run build all pass; settings round-trip tested.

## Remaining
- wails dev verification (theme switch, geometry restore, Save Now); commit.
