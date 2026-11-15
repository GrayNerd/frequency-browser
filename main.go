package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

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
	ctx           context.Context
	config        RadioConfig
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

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Frequency Browser",
		Width:            1024,
		Height:           768,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
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
