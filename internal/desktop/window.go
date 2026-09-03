package desktop

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
)

const minimumVisibleWindowEdge = 64

type windowManager struct {
	app    *application.App
	window *application.WebviewWindow
	store  *windowstate.Store
	logger *log.Logger
}

func newWindowManager(app *application.App, store *windowstate.Store, title string, logger *log.Logger) *windowManager {
	initial := store.Snapshot()
	options := application.WebviewWindowOptions{
		Name:             "main",
		Title:            title,
		URL:              "/",
		Width:            initial.Width,
		Height:           initial.Height,
		MinWidth:         minimumWidth,
		MinHeight:        minimumHeight,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(18, 18, 18),
		KeyBindings:      pageReloadKeyBindings(),
	}
	if initial.HasPosition {
		options.InitialPosition = application.WindowXY
		options.X = initial.X
		options.Y = initial.Y
	}
	if initial.Maximised {
		options.StartState = application.WindowStateMaximised
	}
	manager := &windowManager{app: app, window: app.Window.NewWithOptions(options), store: store, logger: logger}
	manager.bindGeometryEvents()
	return manager
}

func (manager *windowManager) bindGeometryEvents() {
	manager.window.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		manager.captureNormalGeometry()
	})
	manager.window.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		manager.captureNormalGeometry()
	})
	manager.window.OnWindowEvent(events.Common.WindowMaximise, func(*application.WindowEvent) {
		manager.store.Update(func(state *windowstate.State) { state.Maximised = true })
	})
	manager.window.OnWindowEvent(events.Common.WindowUnMaximise, func(*application.WindowEvent) {
		manager.store.Update(func(state *windowstate.State) { state.Maximised = false })
		manager.captureNormalGeometry()
	})
}

func (manager *windowManager) captureNormalGeometry() {
	if manager.window.IsMaximised() || manager.window.IsMinimised() || manager.window.IsFullscreen() {
		return
	}
	width, height := manager.window.Size()
	x, y := manager.window.Position()
	manager.store.Update(func(state *windowstate.State) {
		state.X = x
		state.Y = y
		state.Width = width
		state.Height = height
		state.Maximised = false
		state.HasPosition = true
	})
}

func (manager *windowManager) ensureVisible() {
	state := manager.store.Snapshot()
	if !state.HasPosition || windowIntersectsScreens(state, manager.app.Screen.GetAll()) {
		return
	}
	manager.logger.Printf("saved window position is outside connected displays; centering the window")
	manager.window.Center()
	manager.captureNormalGeometry()
}

func windowIntersectsScreens(state windowstate.State, screens []*application.Screen) bool {
	windowRight := state.X + state.Width
	windowBottom := state.Y + state.Height
	for _, screen := range screens {
		if screen == nil {
			continue
		}
		area := screen.WorkArea
		overlapWidth := min(windowRight, area.X+area.Width) - max(state.X, area.X)
		overlapHeight := min(windowBottom, area.Y+area.Height) - max(state.Y, area.Y)
		if overlapWidth >= minimumVisibleWindowEdge && overlapHeight >= minimumVisibleWindowEdge {
			return true
		}
	}
	return false
}

func (manager *windowManager) save() {
	if manager.window.IsMaximised() {
		manager.store.Update(func(state *windowstate.State) { state.Maximised = true })
	} else {
		manager.captureNormalGeometry()
	}
	if err := manager.store.Save(); err != nil {
		manager.logger.Printf("cannot save window state: %v", err)
	}
}

func (manager *windowManager) close() {
	manager.save()
	manager.window.Hide()
	manager.logger.Printf("desktop window closed; application remains in the system tray")
}

func (manager *windowManager) show() {
	if manager.window.IsMinimised() {
		manager.window.UnMinimise()
	}
	manager.window.Show().Focus()
}

func (manager *windowManager) forceReload() {
	manager.show()
	manager.window.ForceReload()
	manager.logger.Printf("desktop page force-refreshed")
}

func pageReloadKeyBindings() map[string]func(application.Window) {
	reload := func(window application.Window) { window.Reload() }
	return map[string]func(application.Window){"CmdOrCtrl+R": reload, "F5": reload}
}
