package desktop

import (
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	dshdesktop "github.com/the-soloist/dsh-desktop"
	"github.com/the-soloist/dsh-desktop/internal/appicon"
	"github.com/the-soloist/dsh-desktop/internal/backend"
)

type controller struct {
	app                  *application.App
	window               *windowManager
	backend              *backend.Supervisor
	metadata             dshdesktop.Metadata
	logger               *log.Logger
	startup              *startupTimeline
	service              serviceLifecycle
	startupConsoleOwned  bool
	quitting             atomic.Bool
	appStarted           atomic.Bool
	pendingNavigation    atomic.Bool
	smokeScheduled       atomic.Bool
	intentMu             sync.Mutex
	pendingIntent        startupIntent
	navigationMu         sync.Mutex
	navigationCookie     *http.Cookie
	navigationGeneration uint64
	proxyMu              sync.Mutex
	authenticationProxy  *dshAuthenticationProxy
	downloadMu           sync.Mutex
	smokeMu              sync.Mutex
	smokeErr             error
}

func newController(
	app *application.App,
	window *windowManager,
	supervisor *backend.Supervisor,
	metadata dshdesktop.Metadata,
	logger *log.Logger,
	startupConsoleOwned bool,
) *controller {
	return &controller{
		app:                 app,
		window:              window,
		backend:             supervisor,
		metadata:            metadata,
		logger:              logger,
		startup:             newStartupTimeline("正在准备", "即将启动本地 DSH 服务…"),
		startupConsoleOwned: startupConsoleOwned,
		pendingIntent:       startupIntentInitial,
	}
}

func (controller *controller) bind() {
	controller.logger.Printf("[startup] 正在准备 — 即将启动本地 DSH 服务…")
	controller.window.window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if controller.quitting.Load() {
			controller.window.save()
			return
		}
		controller.window.close()
		event.Cancel()
	})

	controller.app.Event.On(startupFrontendReadyEvent, controller.onStartupFrontendReady)
	controller.app.Event.On(startupNavigateEvent, func(event *application.CustomEvent) {
		if event.Sender == "" || event.Sender == controller.window.window.Name() {
			controller.navigateToDSH()
		}
	})
	controller.bindTray()
	controller.app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		controller.window.show()
	})
	controller.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		controller.appStarted.Store(true)
		controller.logger.Printf("[app] desktop window started")
		if controller.quitting.Load() {
			controller.app.Quit()
			return
		}
		if controller.startupConsoleOwned {
			hideStartupConsole()
		}
		controller.window.ensureVisible()
		controller.window.show()
		if smokeTestEnabled() {
			controller.logger.Printf("[smoke] starting DSH without waiting for the startup frontend")
			controller.startPendingService()
		}
	})
	controller.app.OnShutdown(controller.shutdown)
}

func (controller *controller) bindTray() {
	menu := controller.app.NewMenu()
	menu.Add("显示主窗口").OnClick(func(*application.Context) { controller.window.show() })
	menu.Add("强制刷新").OnClick(func(*application.Context) { controller.window.forceReload() })
	menu.Add("重启 DSH").OnClick(func(*application.Context) { controller.requestRestart() })
	menu.Add("关闭窗口").OnClick(func(*application.Context) { controller.window.close() })
	menu.Add("完全退出").OnClick(func(*application.Context) { controller.quit() })
	menu.AddSeparator()
	menu.Add("关于").OnClick(func(*application.Context) { controller.app.Menu.ShowAbout() })
	tray := controller.app.SystemTray.New()
	tray.SetIcon(appicon.PNG)
	tray.SetTooltip(controller.metadata.DisplayName)
	tray.SetMenu(menu)
	tray.OnClick(controller.window.show)
}

func (controller *controller) onStartupFrontendReady(event *application.CustomEvent) {
	if event.Sender != "" && event.Sender != controller.window.window.Name() {
		return
	}
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.snapshot())
	controller.startPendingService()
}

func (controller *controller) startPendingService() {
	switch controller.takeStartupIntent() {
	case startupIntentInitial:
		controller.startService(false)
	case startupIntentRestart:
		controller.startService(true)
	}
}

func (controller *controller) requestRestart() {
	if !controller.service.scheduleRestart() {
		return
	}
	controller.logger.Printf("[dsh] restart requested from tray")
	controller.startup.reset("正在重启 DSH", "正在停止现有服务…", false)
	controller.requestStartupPage(startupIntentRestart)
}

func (controller *controller) requestStartupPage(intent startupIntent) {
	controller.closeAuthenticationProxy()
	controller.pendingNavigation.Store(false)
	controller.navigationMu.Lock()
	controller.navigationGeneration++
	controller.navigationCookie = nil
	controller.navigationMu.Unlock()
	controller.intentMu.Lock()
	controller.pendingIntent = intent
	controller.intentMu.Unlock()
	controller.window.window.SetURL("/")
	controller.window.show()
}

func (controller *controller) takeStartupIntent() startupIntent {
	controller.intentMu.Lock()
	defer controller.intentMu.Unlock()
	intent := controller.pendingIntent
	controller.pendingIntent = startupIntentNone
	return intent
}

func (controller *controller) quit() {
	if !controller.quitting.CompareAndSwap(false, true) {
		return
	}
	controller.service.set(serviceQuitting)
	controller.window.save()
	controller.logger.Printf("[app] complete exit requested")
	if controller.appStarted.Load() {
		controller.app.Quit()
	}
}

func (controller *controller) shutdown() {
	controller.quitting.Store(true)
	controller.closeAuthenticationProxy()
	controller.service.set(serviceQuitting)
	if err := controller.window.store.Save(); err != nil {
		controller.logger.Printf("[app] cannot save window state during shutdown: %v", err)
	}
	if err := controller.backend.Close(); err != nil {
		controller.logger.Printf("[dsh] cannot stop process during shutdown: %v", err)
		controller.recordSmokeFailure(err)
	}
}

func (controller *controller) replaceAuthenticationProxy(proxy *dshAuthenticationProxy) {
	controller.proxyMu.Lock()
	previous := controller.authenticationProxy
	controller.authenticationProxy = proxy
	controller.proxyMu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
}

func (controller *controller) closeAuthenticationProxy() {
	controller.proxyMu.Lock()
	proxy := controller.authenticationProxy
	controller.authenticationProxy = nil
	controller.proxyMu.Unlock()
	if proxy != nil {
		if err := proxy.Close(); err != nil {
			controller.logger.Printf("[dsh] cannot close authentication proxy: %v", err)
		}
	}
}
