package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// App struct
type App struct {
	ctx 		context.Context
    config      RadioConfig
    // rig         RadioInterface
    isConnected bool
	pollingActive bool
	lastMode      string
	}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
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

}

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Frequency Browser",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

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