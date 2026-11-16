//go:build windows

package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// applyWindowTheme makes the native Windows title bar follow the app theme
// (light/dark) and, on Windows 11, uses the exact app colours.
func applyWindowTheme(w *application.WebviewWindow, dark bool, bar, text uint32) {
	if w == nil {
		return
	}
	application.InvokeSync(func() {
		hwnd := uintptr(w.NativeWindow())
		if hwnd == 0 {
			return
		}
		w32.SetTheme(hwnd, dark)
		if w32.SupportsCustomThemes() {
			w32.SetTitleBarColour(hwnd, toColorRef(bar))
			w32.SetTitleTextColour(hwnd, toColorRef(text))
		}
	})
}

// toColorRef converts 0xRRGGBB to Windows COLORREF 0x00BBGGRR.
func toColorRef(rgb uint32) uint32 {
	r, g, b := (rgb>>16)&0xff, (rgb>>8)&0xff, rgb&0xff
	return b<<16 | g<<8 | r
}
