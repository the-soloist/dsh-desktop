package desktop

import (
	"log"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
)

const minimumVisibleWindowEdge = 64

// windowGeometry owns native bounds and their lifecycle, separately from page
// enhancements and DSH layout preferences.
type windowGeometry struct {
	window *application.WebviewWindow
	store  *windowstate.Store
	logger *log.Logger
	ready  atomic.Bool
}

func newWindowGeometry(app *application.App, window *application.WebviewWindow, store *windowstate.Store, initial windowstate.State, logger *log.Logger) *windowGeometry {
	geometry := &windowGeometry{window: window, store: store, logger: logger}
	var once sync.Once
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		once.Do(func() {
			// Establish the normal frame before maximising. Native creation uses
			// different size semantics on macOS, and positioning after zoom can
			// cancel Wails' startup maximisation animation.
			restoreNormalWindowSize(window, initial.Width, initial.Height)
			geometry.ensureVisible(initial, app.Screen.GetAll())
			if initial.Maximised {
				window.Maximise()
			}
			geometry.ready.Store(true)
			geometry.capture()
		})
	})
	for _, event := range []events.WindowEventType{
		events.Common.WindowDidMove, events.Common.WindowDidResize,
		events.Common.WindowMaximise, events.Common.WindowUnMaximise,
	} {
		window.OnWindowEvent(event, func(*application.WindowEvent) { geometry.capture() })
	}
	return geometry
}

func (geometry *windowGeometry) capture() {
	if geometry.record() {
		geometry.store.SaveSoon()
	}
}

func (geometry *windowGeometry) record() bool {
	if !geometry.ready.Load() {
		return false
	}
	if !captureNormalGeometry(geometry.window, geometry.store) {
		width, height := geometry.window.Size()
		if !validWindowSize(width, height) {
			return false
		}
		maximised := geometry.window.IsMaximised()
		geometry.store.Update(func(state *windowstate.State) { state.Maximised = maximised })
	}
	return true
}

func (geometry *windowGeometry) save() {
	geometry.record()
	if err := geometry.store.Save(); err != nil {
		geometry.logger.Printf("[window] cannot save state: %v", err)
	}
}

type geometryWindow interface {
	IsMaximised() bool
	IsMinimised() bool
	IsFullscreen() bool
	Size() (int, int)
	Position() (int, int)
}

func captureNormalGeometry(window geometryWindow, store *windowstate.Store) bool {
	if window.IsMaximised() || window.IsMinimised() || window.IsFullscreen() {
		return false
	}
	width, height := window.Size()
	if !validWindowSize(width, height) {
		return false
	}
	x, y := window.Position()
	store.Update(func(state *windowstate.State) {
		state.X, state.Y = x, y
		state.Width, state.Height = width, height
		state.Maximised = false
		state.HasPosition = true
	})
	return true
}

func validWindowSize(width, height int) bool {
	return width >= minimumWidth && height >= minimumHeight && width <= 10000 && height <= 10000
}

func (geometry *windowGeometry) ensureVisible(state windowstate.State, screens []*application.Screen) {
	if state.HasPosition && len(screens) > 0 && !windowIntersectsScreens(state, screens) {
		geometry.logger.Printf("[window] saved position is outside connected displays; centering")
		geometry.window.Center()
	}
}

func windowIntersectsScreens(state windowstate.State, screens []*application.Screen) bool {
	windowRight, windowBottom := state.X+state.Width, state.Y+state.Height
	for _, screen := range screens {
		if screen == nil {
			continue
		}
		area := screen.WorkArea
		width := min(windowRight, area.X+area.Width) - max(state.X, area.X)
		height := min(windowBottom, area.Y+area.Height) - max(state.Y, area.Y)
		if width >= minimumVisibleWindowEdge && height >= minimumVisibleWindowEdge {
			return true
		}
	}
	return false
}
