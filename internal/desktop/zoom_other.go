//go:build !darwin || server

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// Wails beta.16 clamps SetZoom to >= 1 on WebView2 and WebKitGTK. These backends
// already use layout zoom; do not report a percentage they cannot apply.
const minimumPageZoom = 100

func setNativePageZoom(window *application.WebviewWindow, factor float64) bool {
	window.SetZoom(factor)
	return true
}

func bindNativeZoomMenu(*application.App, *pageZoom) {}
