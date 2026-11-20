package main

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SettingsState is reported by the Settings window whenever its fields change,
// so the backend can decide whether a shutdown needs to prompt the user.
type SettingsState struct {
	Dirty bool `json:"dirty"`
	// Settings is the window's full pending state, saved as-is when the
	// user chooses Save during shutdown.
	Settings Settings `json:"settings"`
}

var (
	settingsStateLock sync.Mutex
	settingsDirty     bool
	settingsPending   Settings

	// shutdownInProgress stops a second close request (menu Exit pressed twice,
	// or the window manager trying again) from running the flow twice.
	shutdownInProgress atomic.Bool

	// allowMainWindowClose is set once the shutdown prompt is resolved, so the
	// close hook lets the main window's WindowClosing event through.
	allowMainWindowClose atomic.Bool
)

// onSettingsState records the Settings window's unsaved state. The pending
// address is kept so the shutdown prompt can save it without racing the
// Settings window's own JavaScript.
func onSettingsState(event *application.CustomEvent) {
	state, ok := decodeSettingsState(event.Data)
	if !ok {
		return
	}
	settingsStateLock.Lock()
	settingsDirty = state.Dirty
	settingsPending = state.Settings
	settingsStateLock.Unlock()
}

// decodeSettingsState accepts the payload in the shapes it can arrive in: a
// real SettingsState (typed dispatch), or the JSON-decoded forms the runtime
// hands over for events emitted from JavaScript.
func decodeSettingsState(data any) (SettingsState, bool) {
	var state SettingsState
	switch v := data.(type) {
	case SettingsState:
		return v, true
	case *SettingsState:
		return *v, true
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return SettingsState{}, false
		}
		if err := json.Unmarshal(b, &state); err != nil {
			return SettingsState{}, false
		}
		return state, true
	case []byte:
		if err := json.Unmarshal(v, &state); err != nil {
			return SettingsState{}, false
		}
		return state, true
	case string:
		if err := json.Unmarshal([]byte(v), &state); err != nil {
			return SettingsState{}, false
		}
		return state, true
	}
	return SettingsState{}, false
}

// resetSettingsState forgets the recorded edits. Called when the Settings
// window closes: whatever was unsaved has been discarded.
func resetSettingsState() {
	settingsStateLock.Lock()
	settingsDirty = false
	settingsPending = Settings{}
	settingsStateLock.Unlock()
}

func getSettingsState() (bool, Settings) {
	settingsStateLock.Lock()
	defer settingsStateLock.Unlock()
	return settingsDirty, settingsPending
}

// onMainWindowClosing intercepts the main window's close so the auxiliary
// windows are closed first and unsaved Settings changes are handled.
//
// The event is cancelled and the shutdown runs asynchronously: the hook runs
// on the UI thread, and blocking there while a dialog waits for input would
// deadlock the application.
func onMainWindowClosing(event *application.WindowEvent) {
	if allowMainWindowClose.Load() {
		return
	}
	event.Cancel()
	requestShutdown()
}

// requestShutdown runs the orderly shutdown sequence: resolve unsaved Settings
// changes, close every auxiliary window, then close the main window.
func requestShutdown() {
	if !shutdownInProgress.CompareAndSwap(false, true) {
		return
	}

	go func() {
		defer shutdownInProgress.Store(false)

		if !confirmUnsavedSettings() {
			// The user chose to keep editing: leave the app running.
			return
		}

		// Record the main window's geometry before anything closes, when
		// the user asked for it in the settings.
		if appService != nil {
			appService.saveWindowGeometryOnExit()
		}

		closeAuxiliaryWindows()

		allowMainWindowClose.Store(true)
		if mainWindow != nil {
			// Closing the last remaining window makes Wails tear the
			// application down, so no explicit Quit is needed (and calling it
			// would run the platform destroy twice).
			mainWindow.Close()
		} else {
			application.Get().Quit()
		}
	}()
}

// confirmUnsavedSettings returns false when the shutdown should be abandoned.
// With no unsaved changes it returns true immediately.
func confirmUnsavedSettings() bool {
	dirty, pending := getSettingsState()
	if !dirty || settingsWindow == nil {
		return true
	}

	choice := make(chan string, 1)
	respond := func(value string) {
		// Non-blocking: a second click (or a duplicate callback) must not
		// leave a goroutine stuck sending to a full channel.
		select {
		case choice <- value:
		default:
		}
	}
	dialog := application.Get().Dialog.Question().
		SetTitle("Unsaved settings").
		SetMessage("The Settings window has unsaved changes. Save them before closing?").
		AttachToWindow(settingsWindow)
	dialog.AddButton("Save").SetAsDefault().OnClick(func() { respond("save") })
	dialog.AddButton("Discard").OnClick(func() { respond("discard") })
	dialog.AddButton("Cancel").SetAsCancel().OnClick(func() { respond("cancel") })
	dialog.Show()

	switch <-choice {
	case "save":
		if appService == nil {
			return true
		}
		if err := appService.SaveSettings(pending); err != nil {
			application.Get().Dialog.Error().
				SetTitle("Could not save settings").
				SetMessage(err.Error()).
				AttachToWindow(settingsWindow).
				Show()
			return false
		}
		return true
	case "discard":
		return true
	default:
		return false
	}
}

// closeAuxiliaryWindows closes every window except the main one, so the main
// window is always the last to go.
func closeAuxiliaryWindows() {
	if settingsWindow != nil {
		settingsWindow.Close()
		settingsWindow = nil
	}
	if importWindow != nil {
		importWindow.Close()
		importWindow = nil
	}
}
