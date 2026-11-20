# Settings Persistence + Window Geometry Handoff

## Status
Persistent settings, tabbed Settings window, light/dark themes, and flash-free
window geometry restore all implemented and verified.

## Done
- settings.config in the user data dir (go-app-paths); auto-created with defaults on first run.
- Settings window: Appearance/Radio tabs; theme (light default), Save Window Size/Position, Save Now; rigctld address.
- Light/dark themes via theme.css; live switch via the themeChanged event.
- Window geometry saved on exit (when enabled) and via Save Now.
- Flash-free restore: window created hidden, realized with Run(), then mapped at the saved position via X11 pre-map placement (window_place_linux.go / showWindowAt). See mainwindow.md.
- go build, go vet, go test -short, and frontend npm run build all pass.

## Remaining
- Commit and push to GitHub.
