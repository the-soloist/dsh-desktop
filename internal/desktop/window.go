package desktop

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
)

type windowManager struct {
	window   *application.WebviewWindow
	logger   *log.Logger
	zoom     *pageZoom
	geometry *windowGeometry
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
	manager := &windowManager{window: app.Window.NewWithOptions(options), logger: logger}
	manager.zoom = newPageZoom(manager.window, logger)
	manager.zoom.bind(app, manager.window)
	manager.geometry = newWindowGeometry(app, manager.window, store, initial, logger)
	manager.bindPageEnhancements()
	return manager
}

func (manager *windowManager) bindPageEnhancements() {
	install := func(*application.WindowEvent) {
		manager.window.ExecJS(desktopBridgeScript + clipboardInterceptorScript + downloadInterceptorScript + layoutAdapterScript + layoutPersistenceScript)
		manager.zoom.restore()
	}
	manager.window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, install)
	manager.window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, install)
	manager.window.OnWindowEvent(events.Linux.WindowLoadFinished, install)
}

func (manager *windowManager) save() {
	manager.geometry.save()
}

func (manager *windowManager) close() {
	manager.save()
	manager.window.Hide()
	manager.logger.Printf("[window] closed; application remains in the system tray")
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
	manager.logger.Printf("[window] page force-refreshed")
}

func pageReloadKeyBindings() map[string]func(application.Window) {
	reload := func(window application.Window) { window.Reload() }
	return map[string]func(application.Window){"CmdOrCtrl+R": reload, "F5": reload}
}
