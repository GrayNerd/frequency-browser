package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// settingsFileName is the name of the persisted settings file. It lives in
// the per-user data directory (on Linux ~/.local/share/frequency-browser/),
// as resolved by go-app-paths.
const settingsFileName = "settings.config"

// Theme values accepted by Settings.Theme.
const (
	ThemeLight = "light"
	ThemeDark  = "dark"
)

// Defaults for settings the config file may not specify: the values that
// were previously hardcoded in main.go.
const (
	defaultWindowWidth  = 1024
	defaultWindowHeight = 768
	defaultRadioAddress = "localhost:4532"
)

// Settings holds every persisted user preference.
//
// WindowSaved distinguishes "no geometry recorded yet" from a real position
// of 0,0, so a first run still centres the window instead of pinning it to
// the top-left corner.
type Settings struct {
	Theme string `json:"theme"`

	// When true the main window's size and position are written back to
	// this file every time the application exits.
	SaveWindowPos bool `json:"saveWindowPos"`

	WindowWidth  int  `json:"windowWidth"`
	WindowHeight int  `json:"windowHeight"`
	WindowX      int  `json:"windowX"`
	WindowY      int  `json:"windowY"`
	WindowSaved  bool `json:"windowSaved"`

	// RadioAddress is the host:port of the rigctld daemon spoken to through
	// hamlib's "hamlib net rigctl" backend.
	RadioAddress string `json:"radioAddress"`

	// ColumnWidths records the user-adjusted width of each Tabulator column,
	// keyed by the column's field name. Persisted so the grid keeps its layout
	// between sessions.
	ColumnWidths map[string]int `json:"columnWidths"`
}

// defaultSettings returns the settings used on first run and as the fallback
// for values missing from the file. Light mode is the default theme and
// window geometry is not tracked until the user asks for it.
func defaultSettings() Settings {
	return Settings{
		Theme:         ThemeLight,
		SaveWindowPos: false,
		WindowWidth:   defaultWindowWidth,
		WindowHeight:  defaultWindowHeight,
		RadioAddress:  defaultRadioAddress,
	}
}

var (
	settingsPathOnce  sync.Once
	settingsPathValue string
	settingsPathErr   error
)

// settingsPath resolves the settings file location once per process, from the
// per-user data directory resolved by go-app-paths.
func settingsPath() (string, error) {
	settingsPathOnce.Do(func() {
		p, err := appDataPath(settingsFileName)
		if err != nil {
			settingsPathErr = fmt.Errorf("could not resolve settings path: %w", err)
			return
		}
		settingsPathValue = p
	})
	return settingsPathValue, settingsPathErr
}

// normalized repairs values that are absent or nonsensical, so a hand-edited
// or partially written config still yields a usable result.
func (s Settings) normalized() Settings {
	if s.Theme != ThemeDark && s.Theme != ThemeLight {
		s.Theme = ThemeLight
	}
	if s.RadioAddress == "" {
		s.RadioAddress = defaultRadioAddress
	}
	if s.WindowWidth <= 0 {
		s.WindowWidth = defaultWindowWidth
	}
	if s.WindowHeight <= 0 {
		s.WindowHeight = defaultWindowHeight
	}
	return s
}

// loadSettings reads the settings file, creating it with defaults when it
// does not exist. A missing, unreadable, or malformed file never stops
// startup: the defaults are used instead.
func loadSettings() Settings {
	path, err := settingsPath()
	if err != nil {
		log.Println("Settings:", err)
		return defaultSettings()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			def := defaultSettings()
			if saveErr := saveSettings(def); saveErr != nil {
				log.Printf("Settings: could not create %s: %v", path, saveErr)
			} else {
				log.Printf("Settings: created %s with defaults", path)
			}
			return def
		}
		log.Printf("Settings: could not read %s: %v", path, err)
		return defaultSettings()
	}

	loaded := defaultSettings()
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("Settings: could not parse %s: %v", path, err)
		return defaultSettings()
	}
	return loaded.normalized()
}

// saveSettings writes the settings atomically: a temporary file in the same
// directory is renamed over the target, so an interrupted write cannot leave
// a truncated file behind.
func saveSettings(s Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("could not create settings directory: %w", err)
	}

	data, err := json.MarshalIndent(s.normalized(), "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode settings: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("could not write settings: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("could not save settings: %w", err)
	}
	return nil
}

// LoadColumnWidths returns the persisted per-column widths, keyed by column
// field name. Returns an empty map when none have been saved yet.
func (a *App) LoadColumnWidths() (map[string]int, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	widths := a.settings.ColumnWidths
	if widths == nil {
		widths = make(map[string]int)
	}
	// Return a copy so the caller can't mutate our internal state.
	out := make(map[string]int, len(widths))
	for k, v := range widths {
		out[k] = v
	}
	return out, nil
}

// SaveColumnWidths merges the given per-column widths into the stored
// settings and persists them. Unlike SaveSettings it does not validate the
// radio address, so resizing a column never trips the rigctld checks.
func (a *App) SaveColumnWidths(widths map[string]int) error {
	a.settingsMu.Lock()
	if a.settings.ColumnWidths == nil {
		a.settings.ColumnWidths = make(map[string]int, len(widths))
	}
	for k, v := range widths {
		a.settings.ColumnWidths[k] = v
	}
	s := a.settings
	a.settingsMu.Unlock()

	return saveSettings(s)
}

// themeBackground returns the native window background colour matching the
// theme, so no flash of the wrong colour is visible before the webview
// paints. The dark value is the one previously hardcoded in main.go.
func themeBackground(theme string) application.RGBA {
	if theme == ThemeDark {
		return application.NewRGB(27, 38, 54)
	}
	return application.NewRGB(245, 246, 248)
}

// currentSettings returns a copy of the live settings.
func (a *App) currentSettings() Settings {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.settings
}

// GetSettings returns the current settings for the Settings window.
func (a *App) GetSettings() Settings {
	return a.currentSettings()
}

// SaveSettings validates and applies new settings from the Settings window
// and persists them. The rigctld address is validated by actually connecting
// (UpdateConfig reverts the radio config itself on failure), so the error
// can be shown as-is.
func (a *App) SaveSettings(s Settings) error {
	s = s.normalized()
	if s.RadioAddress == "" {
		return fmt.Errorf("enter a rigctld address")
	}

	a.settingsMu.Lock()
	previous := a.settings
	a.settings = s
	a.settingsMu.Unlock()

	if err := a.UpdateConfig(s.RadioAddress); err != nil {
		a.settingsMu.Lock()
		a.settings = previous
		a.settingsMu.Unlock()
		return err
	}

	if err := saveSettings(s); err != nil {
		a.settingsMu.Lock()
		a.settings = previous
		a.settingsMu.Unlock()
		return err
	}

	// Tell the open windows about the new theme so they switch immediately.
	if previous.Theme != s.Theme {
		application.Get().Event.Emit("themeChanged", s.Theme)
	}
	return nil
}

// SaveWindowGeometryNow records the main window's current size and position
// in the settings file. Bound to the "Save Now" button in the Appearance
// tab. It returns the captured geometry so the caller can confirm exactly
// what was saved.
func (a *App) SaveWindowGeometryNow() (Settings, error) {
	if mainWindow == nil {
		return Settings{}, fmt.Errorf("main window is not available")
	}
	width, height := mainWindow.Size()
	x, y := mainWindow.Position()
	if width <= 0 || height <= 0 {
		return Settings{}, fmt.Errorf("main window has no usable geometry")
	}

	a.settingsMu.Lock()
	a.settings.WindowWidth = width
	a.settings.WindowHeight = height
	a.settings.WindowX = x
	a.settings.WindowY = y
	a.settings.WindowSaved = true
	s := a.settings
	a.settingsMu.Unlock()

	if err := saveSettings(s); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// saveWindowGeometryOnExit records the main window's geometry when the
// application exits, but only when the user asked for it.
func (a *App) saveWindowGeometryOnExit() {
	a.settingsMu.Lock()
	enabled := a.settings.SaveWindowPos
	a.settingsMu.Unlock()
	if !enabled || mainWindow == nil {
		return
	}
	if _, err := a.SaveWindowGeometryNow(); err != nil {
		log.Printf("Settings: could not save window geometry on exit: %v", err)
	}
}
