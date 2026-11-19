# Dark Mode + Shutdown Handoff

## Status
Dark mode complete. Shutdown-safety work in progress (code done, untested).

## Done
- Dark palette applied app-wide; Tabulator, boxes, inputs, and messages are dark.
- Shutdown now closes Settings/Import windows before the main window (`shutdown.go`).
- Unsaved Settings changes trigger a Save / Discard / Cancel prompt on exit.
- Settings window reports dirty state via the `settingsState` event.

## Remaining
- Run `wails dev`: verify prompt, save-failure handling, and window order.
- Commit changes.
