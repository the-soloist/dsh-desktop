package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// Wails beta.16 dispatches Windows punctuation as OEM_PLUS/OEM_MINUS and
// numpad keys as ADD/SUBTRACT. RegisterKeyBinding parses display accelerators
// and rejects those names; KeyBinding.Add accepts the canonical event strings.
func (zoom *pageZoom) bindWindowsKeys(manager *application.KeyBindingManager, window *application.WebviewWindow) {
	for key, direction := range map[string]int{
		"Ctrl+OEM_PLUS":  1,
		"Ctrl+OEM_MINUS": -1,
		"Ctrl+ADD":       1,
		"Ctrl+SUBTRACT":  -1,
	} {
		manager.Add(key, func(source application.Window) {
			if source != window {
				return
			}
			// Match Wails' window-binding dispatch: never block its native UI
			// thread on zoom.mu while another callback waits for SetZoom.
			go zoom.change(direction)
		})
	}
}
