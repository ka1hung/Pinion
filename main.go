// Pinion — a cross-platform terminal for field engineers: SSH, serial, SFTP and tools.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/ka1hung/pinion/internal/app"
	"github.com/ka1hung/pinion/internal/fonts"
	"github.com/ka1hung/pinion/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dir, err := store.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	core, err := app.NewCore(dir)
	if err != nil {
		log.Fatal(err)
	}

	a := application.New(application.Options{
		Name:        "Pinion",
		Description: "Cross-platform SSH / Serial / Telnet terminal with SFTP, tunnels and quick servers",
		Services:    app.Services(core),
		Assets: application.AssetOptions{
			// Fonts imported in Settings are served from the config dir at /userfonts/.
			Handler: fonts.Handler(fonts.Dir(dir), application.AssetFileServerFS(assets)),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: core.Shutdown,
	})
	core.SetApp(a)

	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Pinion",
		Width:            1280,
		Height:           800,
		MinWidth:         800,
		MinHeight:        500,
		EnableFileDrop:   true,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/",
		Windows: application.WindowsWindow{
			// Start with the saved app theme; the UI then sets exact colours.
			Theme: windowsTheme(core.Store.Settings().Theme),
		},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 52, // matches the toolbar height (Toolbar.tsx)
			TitleBar:                application.MacTitleBarHiddenInset,
		},
	})

	core.Win = win

	// Files dropped from the OS onto an element with data-file-drop-target
	// (e.g. the SFTP panel) are forwarded to the frontend with their paths.
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		payload := map[string]any{"files": e.Context().DroppedFiles()}
		if d := e.Context().DropTargetDetails(); d != nil {
			payload["target"] = d
		}
		a.Event.Emit("files:dropped", payload)
	})

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}

// windowsTheme picks the initial native title bar theme. "system" keeps the
// Windows default; the frontend applies the final colours once loaded.
func windowsTheme(t string) application.Theme {
	switch t {
	case "light":
		return application.Light
	case "dark", "dark-modern":
		return application.Dark
	}
	return application.SystemDefault
}
