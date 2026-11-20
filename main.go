package main

import (
	"context"
	"embed"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

// mainWindow holds the primary application window so other windows (e.g. the
// Settings dialog) can be positioned relative to it.
var mainWindow *application.WebviewWindow

// settingsWindow holds the (single) Settings window while it is open. It is
// reset to nil when the window closes so a later open creates a fresh one.
var settingsWindow *application.WebviewWindow

// importWindow holds the (single) Import window while it is open. It is
// reset to nil when the window closes so a later open creates a fresh one.
var importWindow *application.WebviewWindow

// appService is the single App service instance. Shutdown handling uses it to
// persist settings when the user chooses Save while quitting.
var appService *App

// openImportWindow shows the native "Import" window, creating it on first use.
//
// The window hosts the schedule-import buttons (and their last successful
// download dates) and an OK button that closes it. It is created hidden and
// made a transient child of the main window so it centres on the main window,
// exactly like the Settings window.
func openImportWindow() {
	app := application.Get()
	if importWindow != nil {
		importWindow.Show()
		importWindow.Focus()
		return
	}
	importWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Import",
		Width:  480,
		Height: 220,
		URL:    "/import.html",
		// Themed so the native background matches the webview palette.
		BackgroundColour: themeBackground(appService.currentSettings().Theme),
		Hidden:           true,
		// Fixed size: not resizable, sized so the OK button fits.
		DisableResize: true,
		MinWidth:      480,
		MinHeight:     220,
		MaxWidth:      480,
		MaxHeight:     220,
	})
	importWindow.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		importWindow = nil
	})
	importWindow.SetTransientFor(mainWindow)
	importWindow.Show()
}

// openSettings shows the Settings window, creating it on first use.
func openSettings() {
	app := application.Get()
	if settingsWindow != nil {
		settingsWindow.Show()
		settingsWindow.Focus()
		// Keep it centered on the main window if it drifted.
		if mainWindow != nil {
			mx, my := mainWindow.Position()
			mw, mh := mainWindow.Size()
			sw, sh := settingsWindow.Size()
			settingsWindow.SetPosition(mx+(mw-sw)/2, my+(mh-sh)/2)
		}
		app.Event.Emit("settingsOpened")
		return
	}
	settingsWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Settings",
		Width:  420,
		Height: 320,
		URL:    "/settings.html",
		// Themed so the native background matches the webview palette.
		BackgroundColour: themeBackground(appService.currentSettings().Theme),
		Hidden:           true,
	})
	settingsWindow.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		settingsWindow = nil
		resetSettingsState()
	})
	// Make the Settings window a transient child of the main window so the
	// window manager keeps it on the same monitor (and centers it on the
	// parent) instead of repositioning it arbitrarily.
	if mainWindow != nil {
		settingsWindow.SetTransientFor(mainWindow)
	}
	settingsWindow.Show()
}

// Register the events the backend emits with their data types. This is not
// required, but the binding generator picks up registered events and provides
// a typed JS API for them.
func init() {
	application.RegisterEvent[float64]("frequencyUpdate")
	application.RegisterEvent[string]("modeUpdate")
	application.RegisterEvent[bool]("connectionStatus")
	application.RegisterEvent[SettingsState]("settingsState")
	application.RegisterEvent[string]("themeChanged")
}

// App struct
type App struct {
	ctx    context.Context
	config RadioConfig
	// rig         RadioInterface
	isConnected   bool
	pollingActive bool
	lastMode      string

	// settings holds the persisted user preferences loaded from
	// settings.config at startup. settingsMu guards access from service
	// calls and the shutdown goroutine.
	settings   Settings
	settingsMu sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// ServiceStartup is called when the service starts. The context is saved
// so the poll goroutine can watch for shutdown.
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	a.ctx = ctx
	// Load persisted settings before any window exists: the theme drives
	// the native background colours, the saved geometry sizes the main
	// window, and the radio address configures the rigctld connection. A
	// missing file is created with defaults.
	a.settings = loadSettings()
	a.config.Address = a.settings.RadioAddress // Default rigctld address
	// Merge reference tables cached by a previous HFCC import over the
	// embedded defaults, so new site/broadcaster/country/language codes still
	// resolve when the machine is offline.
	hfccLoadReferenceCache()
	// Same for the tables parsed from EiBi's README.TXT.
	eibiLoadReferenceCache()
	a.pollingActive = true
	go a.pollRadioFrequency()

	return nil
}

func main() {
	appService = NewApp()
	app := application.New(application.Options{
		Name:        "Frequency Browser",
		Description: "Browse shortwave broadcast schedules and tune a rigctld-controlled radio",
		Services: []application.Service{
			application.NewService(appService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})

	// Application menu, shown as a hamburger button in the main window's
	// header bar (GNOME-style primary menu). Menu items run through the app
	// action group; OnClick callbacks fire on activation.
	appMenu := application.NewMenu()
	appMenu.Add("Import").OnClick(func(*application.Context) {
		openImportWindow()
	})
	appMenu.Add("Settings").OnClick(func(*application.Context) {
		openSettings()
	})
	appMenu.Add("Exit").OnClick(func(*application.Context) {
		requestShutdown()
	})
	app.Menu.Set(appMenu)

	// The Settings window reports its unsaved state whenever the address field
	// changes so a shutdown can prompt before discarding those edits.
	app.Event.On("settingsState", onSettingsState)

	app.Event.OnApplicationEvent(events.Linux.ApplicationStartup, func(*application.ApplicationEvent) {
		// Create the main window once the screen list is available. Wails
		// populates its screen cache in its own ApplicationStartup listener,
		// which runs concurrently with this one, so the saved position cannot
		// be converted to screen-relative coordinates until that finishes.
		go createMainWindowWhenScreensReady(app)
	})

	err := app.Run()
	if err != nil {
		println("Error:", err.Error())
	}
}

func (a *App) StartPolling() {
	if !a.pollingActive {
		a.pollingActive = true
		go a.pollRadioFrequency()
	}
}

// createMainWindowWhenScreensReady waits for Wails to finish caching the
// screen list, then creates the main window. The saved window position is
// passed through the creation options (WindowXY) so the window is placed
// correctly by the platform before it is first shown.
func createMainWindowWhenScreensReady(app *application.App) {
	// Wait for the screen cache to be populated (bounded, so a failure to
	// enumerate screens does not hang startup).
	deadline := time.Now().Add(3 * time.Second)
	for len(app.Screen.GetAll()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	application.InvokeSync(func() {
		createMainWindow(app)
	})
}

// createMainWindow builds the main window. It must run on the UI thread.
func createMainWindow(app *application.App) {
	s := appService.currentSettings()

	opts := application.WebviewWindowOptions{
		Title:  "Frequency Browser",
		Width:  s.WindowWidth,
		Height: s.WindowHeight,
		// Themed so the native background matches the webview palette.
		BackgroundColour: themeBackground(s.Theme),
		URL:              "/",
		// Created hidden so the window is never mapped at a default position
		// and then moved (which would flash). It is revealed at the saved
		// position below.
		Hidden: true,
		// Render the application menu as a hamburger button in this window's
		// native header bar instead of a traditional menu bar. The style is
		// needed because the default menu-bar style cannot display plain
		// top-level action items (GTK drops them with a warning).
		UseApplicationMenu: true,
		Linux: application.LinuxWindow{
			MenuStyle: application.LinuxMenuStylePrimaryMenu,
		},
	}

	mainWindow = app.Window.NewWithOptions(opts)
	// Auxiliary windows must be closed before the main window, and an unsaved
	// Settings window gets a say. The hook can cancel the close, so the
	// orderly shutdown happens in requestShutdown instead.
	mainWindow.RegisterHook(events.Common.WindowClosing, onMainWindowClosing)

	// Build the platform implementation while the window is still hidden.
	mainWindow.Run()

	// Reveal the window. With a saved position, showWindowAt maps it directly
	// at those coordinates (X11 pre-map placement) so there is no visible jump
	// from a default location. Without a saved position, or where pre-map
	// placement is unavailable (e.g. Wayland), it is simply shown centred.
	if s.WindowSaved && showWindowAt(mainWindow, s.WindowX, s.WindowY) {
		return
	}
	if s.WindowSaved {
		// Fallback: position could not be applied before mapping, so show and
		// then move (a brief jump is possible on this path).
		mainWindow.Show()
		mainWindow.SetPosition(s.WindowX, s.WindowY)
		return
	}
	mainWindow.Show()
}
