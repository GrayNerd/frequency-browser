# Settings Persistence Handoff

## Status
Implemented and verified: settings persistence, tabbed Settings window, themes, flash-free geometry restore, Restrict Frequency.

## Done
- settings.config in user data dir; defaults on first run.
- Settings window: General/Appearance/Radio tabs; theme, Save Window Size/Position, Save Now; rigctld address.
- General tab: Restrict Frequency to Radio Capabilities; grid filters by rig range only when enabled.
- Themes applied only after Save (no live preview).
- Window geometry saved on exit and via Save Now.
- Flash-free restore via X11 pre-map placement.
- go build, go test pass.
- Committed and pushed to origin/wails3.