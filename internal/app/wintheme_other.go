//go:build !windows

package app

import "github.com/wailsapp/wails/v3/pkg/application"

// applyWindowTheme is a no-op outside Windows (macOS uses a hidden-inset
// title bar drawn by the app; Linux follows the GTK theme).
func applyWindowTheme(w *application.WebviewWindow, dark bool, bar, text uint32) {}
