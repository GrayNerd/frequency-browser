# Consolidated Checkpoint Summary

## Objective
- Keep the app in a complete dark mode.
- Shut down cleanly: close auxiliary windows before the main window, and protect unsaved Settings changes.
- Keep `handoff.md` in the project root under 100 words.

## Requirements
- Frequency/time boxes and Tabulator header/rows dark with readable light text.
- On shutdown, close every other app window before the main window.
- If Settings has unsaved changes, prompt to save or cancel them.
- `handoff.md` at `/home/ken/src/frequency-browser/handoff.md`, under 100 words.

## Decisions
- Reuse the shared palette: `--bg #1b2636`, `--bg-input #243447`, `--fg #e6e9ef`, `--fg-dim #9aa7b8`, `--accent #4a90d9`, `--danger #e06c75`, `--border #35455c`.
- Unsaved-state reporting uses the `settingsState` Wails event (avoids regenerating bindings).
- Shutdown prompt offers Save / Discard / Cancel; Cancel aborts the shutdown so edits can continue.
- Intercept `WindowClosing` with a cancellable hook (`RegisterHook`) and run the flow in a goroutine to avoid blocking GTK.

## Work State
### Completed
- Dark mode: `style.css` palette, `App.svelte` boxes/inputs/labels/Tabulator/messages, error/success/debug styling.
- `shutdown.go`: `onMainWindowClosing`, `requestShutdown`, `confirmUnsavedSettings`, `closeAuxiliaryWindows`, settings-state tracking.
- `main.go`: main window hook registered, Exit menu routed through `requestShutdown`, `settingsState` event wiring, service instance stored in `appService`.
- `settings.js`: tracks `cleanAddress`, emits dirty state on load/input/save, and re-raising Settings no longer overwrites unsaved edits.
- `handoff.md` (83 words) and this checkpoint updated.
- `go build`, `go test ./...`, and `frontend npm run build` all succeed.

### Active
- Awaiting runtime verification with `wails dev`.

### Blocked
- None.

## Next Move
1. Run `wails dev`; edit Settings, then Exit and exercise Save, Discard, and Cancel.
2. Confirm Settings/Import close before the main window (including the main window X button).
3. Commit `main.go`, `shutdown.go`, `App.svelte`, `style.css`, `settings.js`, `handoff.md`, `checkpoint.md`.

## Relevant Files
- `main.go`: window creation, menu, event wiring, close hook.
- `shutdown.go`: shutdown sequence and unsaved-settings prompt.
- `frontend/src/settings.js`: dirty-state tracking and reporting.
- `frontend/src/App.svelte`, `frontend/src/style.css`: dark mode.
- `handoff.md`, `checkpoint.md`: handoff and consolidated state.

## Important Context
- Native window background is RGB(27, 38, 54) in `main.go`.
- Dialog buttons: "Cancel" is the GTK cancel button, triggered by Escape/window close.
- `UpdateConfig` validates by connecting to the rig and reverts on failure; a failed save during shutdown shows an error and aborts the quit.
- No changes committed yet.
