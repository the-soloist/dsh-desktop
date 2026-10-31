//go:build !darwin || server

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

func restoreNormalWindowSize(window *application.WebviewWindow, width, height int) {
	window.SetSize(width, height)
}
