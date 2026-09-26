package desktop

import (
	_ "embed"
	"fmt"
	"log"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed zoom.js
var zoomIndicatorScript string

const maximumPageZoom = 200

// pageZoom owns the level across documents, including startup -> DSH and reloads.
// Keyboard/menu callbacks and navigation events may arrive on different goroutines.
type pageZoom struct {
	mu      sync.Mutex
	percent int
	apply   func(int, bool) bool
}

func newPageZoom(window *application.WebviewWindow, logger *log.Logger) *pageZoom {
	return &pageZoom{percent: 100, apply: func(percent int, show bool) bool {
		if !setNativePageZoom(window, float64(percent)/100) {
			logger.Printf("[window] cannot set page zoom: WebView is not available")
			return false
		}
		window.ExecJS(fmt.Sprintf("(%s)(%d,%t)", zoomIndicatorScript, percent, show))
		return true
	}}
}

func (zoom *pageZoom) bind(app *application.App, window *application.WebviewWindow) {
	for key, callback := range zoom.keyBindings() {
		window.RegisterKeyBinding(key, callback)
	}
	bindNativeZoomMenu(app, zoom)
}

func (zoom *pageZoom) keyBindings() map[string]func(application.Window) {
	return zoom.keyBindingsForOS(runtime.GOOS)
}

func (zoom *pageZoom) keyBindingsForOS(goos string) map[string]func(application.Window) {
	in := func(application.Window) { zoom.change(1) }
	out := func(application.Window) { zoom.change(-1) }
	modifiers := []string{"Ctrl"}
	if goos == "darwin" {
		// Command is the macOS shortcut; keep the existing Control aliases.
		modifiers = []string{"Cmd", "Ctrl"}
	}
	bindings := make(map[string]func(application.Window))
	for _, modifier := range modifiers {
		for _, key := range []string{"=", "plus"} {
			bindings[modifier+"+"+key] = in
		}
		bindings[modifier+"+-"] = out
	}
	return bindings
}

func nextPageZoom(current, direction, minimum int) int {
	if direction == 0 {
		return 100
	}
	return max(minimum, min(maximumPageZoom, current+direction*10))
}

func (zoom *pageZoom) change(direction int) {
	zoom.mu.Lock()
	defer zoom.mu.Unlock()
	percent := nextPageZoom(zoom.percent, direction, minimumPageZoom)
	if zoom.apply(percent, true) {
		zoom.percent = percent
	}
}

func (zoom *pageZoom) restore() {
	zoom.mu.Lock()
	defer zoom.mu.Unlock()
	zoom.apply(zoom.percent, false)
}
