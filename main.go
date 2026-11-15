package main

import (
	"context"
	"embed"
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
		Hidden: true,
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
		Height: 220,
		URL:    "/settings.html",
		Hidden: true,
	})
	settingsWindow.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		settingsWindow = nil
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
}

// App struct
type App struct {
	ctx    context.Context
	config RadioConfig
	// rig         RadioInterface
	isConnected   bool
	pollingActive bool
	lastMode      string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// ServiceStartup is called when the service starts. The context is saved
// so the poll goroutine can watch for shutdown.
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	a.ctx = ctx
	a.config.Address = "localhost:4532" // Default rigctld address
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
	app := application.New(application.Options{
		Name:        "Frequency Browser",
		Description: "Browse shortwave broadcast schedules and tune a rigctld-controlled radio",
		Services: []application.Service{
			application.NewService(&App{}),
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
		application.Get().Quit()
	})
	app.Menu.Set(appMenu)

	app.Event.OnApplicationEvent(events.Linux.ApplicationStartup, func(*application.ApplicationEvent) {
		mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            "Frequency Browser",
			Width:            1024,
			Height:           768,
			BackgroundColour: application.NewRGB(27, 38, 54),
			URL:              "/",
			// Render the application menu as a hamburger button in this
			// window's native header bar instead of a traditional menu bar.
			// The style is needed because the default menu-bar style cannot
			// display plain top-level action items (GTK drops them with a
			// warning).
			UseApplicationMenu: true,
			Linux: application.LinuxWindow{
				MenuStyle: application.LinuxMenuStylePrimaryMenu,
			},
		})
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
