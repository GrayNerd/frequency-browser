package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

	// defaultRemoteAddress is what the Network Address field is pre-filled
	// with for a network backend. It uses the name "localhost" rather than
	// this machine's IP: a network rig usually runs on the same host, and
	// localhost keeps working when the machine's address changes.
	defaultRemoteAddress = "localhost:4532"

	// First-run defaults for the filter modes.
	defaultFrequencyFilterMode = "near" // "exact", "near", "all", or "manual"
	defaultCurrentTimeFilter   = "now"  // "now", "soon", "all", or "manual"

	// Defaults for the managed rigctld daemon started from the Radio tab.
	defaultBaudRate       = 19200
	defaultPollIntervalMs = 500
)

// baudRateChoices are the serial speeds offered in the Radio tab.
var baudRateChoices = []int{1200, 2400, 4800, 9600, 14400, 19200, 28800, 38400, 57600, 76800, 115200}

// pollIntervalChoices are the rig polling speeds offered in the Radio tab, in
// milliseconds. A stored value of 0 means "Never": the rig is not polled.
var pollIntervalChoices = []int{250, 500, 1000, 2000, 5000}

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
	// hamlib's "hamlib net rigctl" backend. It is no longer shown in the UI:
	// the app starts its own daemon on the loopback address.
	RadioAddress string `json:"radioAddress"`

	// RigModel is hamlib's internal model number for the attached radio, the
	// value rigctld's -m option takes (3078 is the Icom IC-7610). Zero means
	// the radio has not been configured yet, in which case no daemon is
	// started and the app only adopts one that is already running.
	RigModel int `json:"rigModel"`

	// SerialPort and the parameters below are passed straight through to
	// rigctld. An empty value means "Default": that option is left off the
	// command line so hamlib's own default applies.
	// RemoteAddress is the host:port of a rigctld daemon to control when the
	// chosen rig model is hamlib's "NET rigctl" backend. That backend has no
	// serial port: its -r argument is the address of the remote daemon.
	RemoteAddress string `json:"remoteAddress"`

	SerialPort string `json:"serialPort"` // e.g. /dev/ttyUSB0
	BaudRate   int    `json:"baudRate"`   // one of baudRateChoices
	DataBits   string `json:"dataBits"`   // "" (default) | "7" | "8"
	StopBits   string `json:"stopBits"`   // "" (default) | "1" | "2"
	Handshake  string `json:"handshake"`  // "" (default) | "None" | "XONXOFF" | "Hardware"
	DTR        string `json:"dtr"`        // "" (blank) | "High" | "Low"
	RTS        string `json:"rts"`        // "" (blank) | "High" | "Low"

	// PollIntervalMs is how often the rig frequency is read: one of
	// pollIntervalChoices, or 0 for "Never" (no polling at all).
	PollIntervalMs int `json:"pollIntervalMs"`

	// FrequencyFilterMode is the persisted frequency input mode: "exact",
	// "near", "all", or "manual".
	FrequencyFilterMode string `json:"frequencyFilterMode"`

	// CurrentTimeFilter is the persisted time filter mode: "now", "soon",
	// "all", or "manual".
	CurrentTimeFilter string `json:"currentTimeFilter"`

	// ColumnWidths records the user-adjusted width of each Tabulator column,
	// keyed by the column's field name. Persisted so the grid keeps its layout
	// between sessions.
	ColumnWidths map[string]int `json:"columnWidths"`

	// ColumnOrder records the user-adjusted order of the Tabulator columns,
	// as field names. Persisted so the grid keeps its layout between sessions.
	ColumnOrder []string `json:"columnOrder"`
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

		// No rig is chosen on first run: the user picks one in Settings,
		// Radio. The remaining serial parameters keep hamlib's defaults.
		RigModel:       0,
		RemoteAddress:  defaultRemoteAddress,
		BaudRate:       defaultBaudRate,
		PollIntervalMs: defaultPollIntervalMs,

		// First-run defaults: near the tuned frequency, and "now" for time.
		FrequencyFilterMode: defaultFrequencyFilterMode,
		CurrentTimeFilter:   defaultCurrentTimeFilter,
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
	s = normalizeRadio(s)
	if s.FrequencyFilterMode != "exact" && s.FrequencyFilterMode != "near" &&
		s.FrequencyFilterMode != "all" && s.FrequencyFilterMode != "manual" {
		s.FrequencyFilterMode = defaultFrequencyFilterMode
	}
	if s.CurrentTimeFilter != "now" && s.CurrentTimeFilter != "soon" &&
		s.CurrentTimeFilter != "all" && s.CurrentTimeFilter != "manual" {
		s.CurrentTimeFilter = defaultCurrentTimeFilter
	}
	if s.WindowWidth <= 0 {
		s.WindowWidth = defaultWindowWidth
	}
	if s.WindowHeight <= 0 {
		s.WindowHeight = defaultWindowHeight
	}
	return s
}

// normalizeRadio repairs the Radio tab's values, so a hand-edited config or a
// version of hamlib without a given option still yields a usable daemon command
// line. Unknown choices fall back to "Default" (the option is omitted).
func normalizeRadio(s Settings) Settings {
	if s.RigModel < 0 {
		s.RigModel = 0
	}
	if !containsInt(baudRateChoices, s.BaudRate) {
		s.BaudRate = defaultBaudRate
	}
	if !validOrDefault(s.DataBits, "7", "8") {
		s.DataBits = ""
	}
	if !validOrDefault(s.StopBits, "1", "2") {
		s.StopBits = ""
	}
	if !validOrDefault(s.Handshake, "None", "XONXOFF", "Hardware") {
		s.Handshake = ""
	}
	if !validOrDefault(s.DTR, "High", "Low") {
		s.DTR = ""
	}
	if !validOrDefault(s.RTS, "High", "Low") {
		s.RTS = ""
	}
	s.RemoteAddress = strings.TrimSpace(s.RemoteAddress)
	if s.PollIntervalMs != 0 && !containsInt(pollIntervalChoices, s.PollIntervalMs) {
		s.PollIntervalMs = defaultPollIntervalMs
	}
	return s
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// validOrDefault reports whether v is empty ("Default") or one of allowed.
func validOrDefault(v string, allowed ...string) bool {
	if v == "" {
		return true
	}
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// radioChanged reports whether any Radio tab field differs, which is what
// decides if the daemon has to be restarted.
func radioChanged(before, after Settings) bool {
	return before.RigModel != after.RigModel ||
		before.RemoteAddress != after.RemoteAddress ||
		before.SerialPort != after.SerialPort ||
		before.BaudRate != after.BaudRate ||
		before.DataBits != after.DataBits ||
		before.StopBits != after.StopBits ||
		before.Handshake != after.Handshake ||
		before.DTR != after.DTR ||
		before.RTS != after.RTS
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

// LoadColumnOrder returns the persisted column order as field names, or an
// empty slice when none has been saved yet.
func (a *App) LoadColumnOrder() ([]string, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	order := a.settings.ColumnOrder
	if order == nil {
		order = make([]string, 0)
	}
	out := make([]string, len(order))
	copy(out, order)
	return out, nil
}

// SaveColumnOrder replaces the stored column order with the given field names
// and persists them.
func (a *App) SaveColumnOrder(order []string) error {
	a.settingsMu.Lock()
	out := make([]string, len(order))
	copy(out, order)
	a.settings.ColumnOrder = out
	s := a.settings
	a.settingsMu.Unlock()

	return saveSettings(s)
}

// SaveFilterOptions persists the frequency and time filter modes so they
// survive between sessions. Unlike SaveSettings it does not validate the
// rigctld address, so changing a filter never trips the radio checks.
func (a *App) SaveFilterOptions(frequencyFilterMode, currentTimeFilter string) error {
	a.settingsMu.Lock()
	a.settings.FrequencyFilterMode = frequencyFilterMode
	a.settings.CurrentTimeFilter = currentTimeFilter
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

// emitEvent sends a backend event to the open windows. It is a no-op when no
// application exists yet, so service methods stay usable from tests.
func emitEvent(name string, data any) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(name, data)
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

// SaveSettings validates and applies new settings from the Settings window and
// persists them.
//
// When a Radio field changed the rigctld daemon is restarted with the new
// options, and the save only takes effect once that daemon is up: a rejected
// command line (wrong rig for the port, unplugged adapter) reverts everything
// and returns hamlib's own message, which the dialog shows verbatim.
func (a *App) SaveSettings(s Settings) error {
	s = s.normalized()

	a.settingsMu.Lock()
	previous := a.settings
	a.settings = s
	a.settingsMu.Unlock()
	// The rig client always talks to the address in the settings, whether or not
	// this save touched the Radio tab.
	a.config.Address = s.RadioAddress

	// Pressing Save must leave a working daemon behind: restart it when the
	// options changed, and start one if none is running (for example on a first
	// run, or after it failed to come up).
	needDaemon := radioChanged(previous, s) ||
		(s.RigModel > 0 && !a.probeDaemon(s.RadioAddress))

	if needDaemon {
		if err := a.restartDaemon(s); err != nil {
			// The old daemon is already gone, so put the previous settings
			// back in force and restart from them: a rejected change must not
			// leave the app with no radio at all.
			a.settingsMu.Lock()
			a.settings = previous
			a.config.Address = previous.RadioAddress
			a.settingsMu.Unlock()
			if restoreErr := a.restartDaemon(previous); restoreErr != nil {
				log.Printf("rigctld: could not restore the previous daemon: %v", restoreErr)
			}
			return err
		}
	}

	if err := saveSettings(s); err != nil {
		a.settingsMu.Lock()
		a.settings = previous
		a.settingsMu.Unlock()
		return err
	}

	if needDaemon || previous.PollIntervalMs != s.PollIntervalMs {
		// Wake the poll loop so a changed interval (or "Never") applies at
		// once, and tell the main window to re-read the rig.
		a.wakePolling()
	}
	if needDaemon {
		emitEvent("radioRestarted", true)
	}

	// Tell the open windows about the new theme so they switch immediately.
	if previous.Theme != s.Theme {
		emitEvent("themeChanged", s.Theme)
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
