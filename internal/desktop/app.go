package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	dshdesktop "github.com/the-soloist/dsh-desktop"
	"github.com/the-soloist/dsh-desktop/internal/appicon"
)

const (
	appName            = "DshDesktop"
	appDescription     = "Desktop client for DeepSeek DSH"
	dshPackage         = "@deepseek-ai/dsh@latest"
	dshURL             = "http://127.0.0.1:3080"
	defaultWidth       = 1200
	defaultHeight      = 780
	minimumWidth       = 800
	minimumHeight      = 560
	defaultStartWait   = 5 * time.Minute
	readinessInterval  = 250 * time.Millisecond
	readinessStability = time.Second
	requestTimeout     = 2 * time.Second
	processStopTimeout = 5 * time.Second
)

type windowState struct {
	X           int  `json:"x"`
	Y           int  `json:"y"`
	Width       int  `json:"width"`
	Height      int  `json:"height"`
	Maximised   bool `json:"maximised"`
	HasPosition bool `json:"hasPosition"`
}

type stateStore struct {
	path  string
	mu    sync.Mutex
	state windowState
}

type managedProcess struct {
	cmd      *exec.Cmd
	done     chan struct{}
	mu       sync.Mutex
	waitErr  error
	stopOnce sync.Once
}

type managedProcessState struct {
	mu      sync.Mutex
	current *managedProcess
}

func newManagedProcessState(process *managedProcess) *managedProcessState {
	return &managedProcessState{current: process}
}

func (state *managedProcessState) set(process *managedProcess) {
	state.mu.Lock()
	state.current = process
	state.mu.Unlock()
}

func (state *managedProcessState) take() *managedProcess {
	state.mu.Lock()
	defer state.mu.Unlock()
	process := state.current
	state.current = nil
	return process
}

func (state *managedProcessState) clearIfCurrent(process *managedProcess) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.current != process {
		return false
	}
	state.current = nil
	return true
}

// Main starts the desktop application and reports fatal startup errors using a
// platform-native warning dialog.
func Main() {
	if err := run(); err != nil {
		message := err.Error()
		log.Printf("fatal: %s", message)
		showPlatformWarning(appName, message)
		os.Exit(1)
	}
}

func run() error {
	startupConsoleOwned := ownsStartupConsole()
	logger, closeLog, err := newLauncherLogger()
	if err != nil {
		return fmt.Errorf("cannot initialise the launcher log: %w", err)
	}
	defer closeLog()
	version, err := dshdesktop.CurrentVersion()
	if err != nil {
		return fmt.Errorf("cannot determine application version: %w", err)
	}
	aboutDescription := fmt.Sprintf("%s\nVersion %s", appDescription, version)

	logger.Printf("starting %s on %s/%s", appName, runtime.GOOS, runtime.GOARCH)

	statePath, err := windowStatePath()
	if err != nil {
		return fmt.Errorf("cannot resolve the window-state path: %w", err)
	}
	store := newStateStore(statePath, logger)
	initialState := store.snapshot()
	startupState := newStartupTimeline("正在准备", "即将启动本地 DSH 服务…")

	app := application.New(application.Options{
		Name:        appName,
		Description: aboutDescription,
		Icon:        appicon.PNG,
		Assets: application.AssetOptions{
			Handler:        startupAssetHandler(),
			DisableLogging: true,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ProgramName: appName},
	})

	windowOptions := application.WebviewWindowOptions{
		Name:             "main",
		Title:            "DSH Desktop",
		URL:              "/",
		Width:            initialState.Width,
		Height:           initialState.Height,
		MinWidth:         minimumWidth,
		MinHeight:        minimumHeight,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(18, 18, 18),
		KeyBindings:      pageReloadKeyBindings(),
	}
	if initialState.HasPosition {
		windowOptions.InitialPosition = application.WindowXY
		windowOptions.X = initialState.X
		windowOptions.Y = initialState.Y
	}
	if initialState.Maximised {
		windowOptions.StartState = application.WindowStateMaximised
	}

	window := app.Window.NewWithOptions(windowOptions)
	logger.Printf("startup status: 正在准备 — 即将启动本地 DSH 服务…")
	backendState := newManagedProcessState(nil)
	var quitting atomic.Bool
	var appStarted atomic.Bool
	var serviceActionRunning atomic.Bool
	var pendingDSHNavigation atomic.Bool
	var smokeScheduled atomic.Bool
	var smokeFailureMu sync.Mutex
	var smokeFailure error
	var startupIntentMu sync.Mutex
	pendingStartupIntent := startupIntentInitial
	captureNormalGeometry := func() {
		if window.IsMaximised() || window.IsMinimised() || window.IsFullscreen() {
			return
		}
		width, height := window.Size()
		x, y := window.Position()
		store.update(func(state *windowState) {
			state.X = x
			state.Y = y
			state.Width = width
			state.Height = height
			state.Maximised = false
			state.HasPosition = true
		})
	}

	window.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		captureNormalGeometry()
	})
	window.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		captureNormalGeometry()
	})
	window.OnWindowEvent(events.Common.WindowMaximise, func(*application.WindowEvent) {
		store.update(func(state *windowState) { state.Maximised = true })
	})
	window.OnWindowEvent(events.Common.WindowUnMaximise, func(*application.WindowEvent) {
		store.update(func(state *windowState) { state.Maximised = false })
		captureNormalGeometry()
	})
	saveWindowState := func() {
		if window.IsMaximised() {
			store.update(func(state *windowState) { state.Maximised = true })
		} else {
			captureNormalGeometry()
		}
		if saveErr := store.save(); saveErr != nil {
			logger.Printf("cannot save window state: %v", saveErr)
		}
	}
	closeWindow := func() {
		saveWindowState()
		window.Hide()
		logger.Printf("desktop window closed; application remains in the system tray")
	}
	showWindow := func() {
		if window.IsMinimised() {
			window.UnMinimise()
		}
		window.Show().Focus()
	}
	requestStartupPage := func(intent startupIntent) {
		pendingDSHNavigation.Store(false)
		startupIntentMu.Lock()
		pendingStartupIntent = intent
		startupIntentMu.Unlock()
		window.SetURL("/")
		showWindow()
	}
	takeStartupIntent := func() startupIntent {
		startupIntentMu.Lock()
		defer startupIntentMu.Unlock()
		intent := pendingStartupIntent
		pendingStartupIntent = startupIntentNone
		return intent
	}
	forceRefreshPage := func() {
		showWindow()
		window.ForceReload()
		logger.Printf("desktop page force-refreshed")
	}
	quitApplication := func() {
		if !quitting.CompareAndSwap(false, true) {
			return
		}
		saveWindowState()
		logger.Printf("complete application exit requested")
		if appStarted.Load() {
			app.Quit()
		}
	}

	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if quitting.Load() {
			saveWindowState()
			return
		}
		closeWindow()
		event.Cancel()
	})

	setStartupStatus := func(status, detail string, failed bool) {
		logger.Printf("startup status: %s — %s", status, strings.ReplaceAll(detail, "\n", " "))
		window.EmitEvent(startupUpdateEvent, startupState.append(status, detail, failed, false))
	}
	recordSmokeFailure := func(failure error) {
		if !smokeTestEnabled() || failure == nil {
			return
		}
		smokeFailureMu.Lock()
		if smokeFailure == nil {
			smokeFailure = failure
		}
		smokeFailureMu.Unlock()
	}
	scheduleSmokeFailureExit := func() {
		if smokeTestEnabled() && smokeScheduled.CompareAndSwap(false, true) {
			time.AfterFunc(time.Second, quitApplication)
		}
	}
	scheduleSmokeSuccess := func() {
		if !smokeTestEnabled() || !smokeScheduled.CompareAndSwap(false, true) {
			return
		}
		duration := smokeTestDuration()
		closeDelay := time.Second
		if duration <= 2*time.Second {
			closeDelay = duration / 2
		}
		logger.Printf("smoke test will close the window after %s and exit after %s", closeDelay, duration)
		time.AfterFunc(closeDelay, window.Close)
		time.AfterFunc(duration, quitApplication)
	}
	navigateToDSH := func() {
		if quitting.Load() || !pendingDSHNavigation.CompareAndSwap(true, false) {
			return
		}
		window.SetURL(dshURL)
		showWindow()
		scheduleSmokeSuccess()
	}
	showStartupFailure := func(summary string, failure error) {
		detail := summary
		if failure != nil {
			detail += "\n\n" + failure.Error()
		}
		logger.Printf("DSH startup failed: %s", detail)
		setStartupStatus("DSH 启动失败", detail+"\n\n请修复问题后从托盘菜单选择“重启 DSH”。", true)
		showWindow()
		recordSmokeFailure(errors.New(detail))
		scheduleSmokeFailureExit()
	}
	showDSH := func(message string) {
		logger.Printf("DSH is ready at %s", dshURL)
		pendingDSHNavigation.Store(true)
		window.EmitEvent(startupUpdateEvent, startupState.append("DSH 已就绪", message, false, true))
		time.AfterFunc(5*time.Second, navigateToDSH)
	}

	monitorBackend := func(process *managedProcess) {
		go func() {
			<-process.done
			if !backendState.clearIfCurrent(process) || quitting.Load() {
				return
			}
			message := "DSH 服务已停止。请从托盘菜单选择“重启 DSH”。"
			if processErr := process.waitError(); processErr != nil {
				logger.Printf("DSH process exited: %v", processErr)
				message += "\n\n" + processErr.Error()
				recordSmokeFailure(fmt.Errorf("DSH process exited: %w", processErr))
			} else {
				logger.Printf("DSH process exited")
				recordSmokeFailure(errors.New("DSH process exited"))
			}
			logger.Printf("startup status: DSH 已停止 — %s", strings.ReplaceAll(message, "\n", " "))
			startupState.reset("DSH 已停止", message, true)
			requestStartupPage(startupIntentNone)
			scheduleSmokeFailureExit()
		}()
	}
	startDSHAction := func(restart bool) {
		if !serviceActionRunning.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer serviceActionRunning.Store(false)

			if restart {
				logger.Printf("DSH restart requested")
				current := backendState.take()
				if current == nil && isDSHReady() {
					showPlatformWarning(appName, "当前 DSH 服务由外部进程启动，DSH Desktop 不会终止它。")
					window.SetURL(dshURL)
					showWindow()
					return
				}
				if current != nil {
					current.Stop(logger)
				}
			} else {
				setStartupStatus("正在检查本地服务", "正在检测 127.0.0.1:3080…", false)
				if isDSHReady() {
					setStartupStatus("正在连接 DSH", "检测到已有服务，正在确认其稳定性…", false)
					stableWait := readinessStability + 2*readinessInterval
					if stableErr := waitForDSHWithProbe(nil, stableWait, readinessInterval, readinessStability, isDSHReady); stableErr == nil {
						logger.Printf("reusing service already listening at %s", dshURL)
						showDSH("正在加载现有 DSH 服务…")
						return
					}
					logger.Printf("existing DSH service disappeared during readiness check")
				}
			}

			if quitting.Load() {
				return
			}
			setStartupStatus("正在检查运行环境", "正在查找 bunx 与 Node.js…", false)
			bunxPath, findErr := findBunx()
			if findErr != nil {
				showStartupFailure("未找到 bunx。请先安装 Bun，并确保 bunx 已加入 PATH。", findErr)
				return
			}
			logger.Printf("using bunx: %s", bunxPath)

			environment, dshHome := dshEnvironment(os.Environ())
			environment = prependExecutablePaths(environment, executableSearchPaths(bunxPath)...)
			if dshHome != "" {
				logger.Printf("using DSH_HOME: %s", dshHome)
			}
			logger.Printf("DSH executable search path: %s", environmentValue(environment, "PATH"))
			workspace, workspaceErr := dshWorkspace()
			if workspaceErr != nil {
				showStartupFailure("无法确定 DSH 工作目录。", workspaceErr)
				return
			}

			setStartupStatus("正在启动 DSH", "正在后台启动本地 Web 服务…", false)
			logger.Printf("starting DSH in %s", workspace)
			summaryLines := make(chan string, 32)
			output := newStartupOutputRecorder(logger.Writer(), func(line string) {
				select {
				case summaryLines <- line:
				default:
				}
			})
			next, startErr := startDSH(bunxPath, workspace, environment, output)
			if startErr != nil {
				showStartupFailure("无法启动 DSH。", startErr)
				return
			}
			backendState.set(next)
			if quitting.Load() {
				if backendState.clearIfCurrent(next) {
					next.Stop(logger)
				}
				return
			}

			setStartupStatus("正在等待 DSH", "DSH 进程已启动，正在等待服务和插件完成初始化…", false)
			summaryDone := make(chan struct{})
			go func() {
				reported := make(map[string]struct{})
				for {
					select {
					case line := <-summaryLines:
						key, status, detail, ok := dshOutputSummary(line)
						if !ok {
							continue
						}
						if _, exists := reported[key]; exists {
							continue
						}
						reported[key] = struct{}{}
						setStartupStatus(status, detail, false)
					case <-summaryDone:
						return
					case <-next.done:
						return
					}
				}
			}()
			waitErr := waitForDSH(next, startTimeout())
			close(summaryDone)
			if waitErr != nil {
				if backendState.clearIfCurrent(next) {
					next.Stop(logger)
				}
				if quitting.Load() {
					return
				}
				if recentOutput := output.recentOutput(); recentOutput != "" {
					waitErr = fmt.Errorf("%w\n\n最近的 DSH 输出：\n%s", waitErr, recentOutput)
				}
				showStartupFailure("DSH 未能完成启动。", waitErr)
				return
			}
			if quitting.Load() {
				if backendState.clearIfCurrent(next) {
					next.Stop(logger)
				}
				return
			}
			monitorBackend(next)
			showDSH("启动完成，正在加载界面…")
		}()
	}
	requestDSHRestart := func() {
		if serviceActionRunning.Load() {
			return
		}
		logger.Printf("DSH restart requested from tray")
		startupState.reset("正在重启 DSH", "正在停止现有服务…", false)
		requestStartupPage(startupIntentRestart)
	}

	app.Event.On(startupFrontendReadyEvent, func(event *application.CustomEvent) {
		if event.Sender != "" && event.Sender != window.Name() {
			return
		}
		window.EmitEvent(startupUpdateEvent, startupState.snapshot())
		switch takeStartupIntent() {
		case startupIntentInitial:
			startDSHAction(false)
		case startupIntentRestart:
			startDSHAction(true)
		}
	})
	app.Event.On(startupNavigateEvent, func(event *application.CustomEvent) {
		if event.Sender == "" || event.Sender == window.Name() {
			navigateToDSH()
		}
	})

	trayMenu := app.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(*application.Context) { showWindow() })
	trayMenu.Add("强制刷新").OnClick(func(*application.Context) { forceRefreshPage() })
	trayMenu.Add("重启 DSH").OnClick(func(*application.Context) { requestDSHRestart() })
	trayMenu.Add("关闭窗口").OnClick(func(*application.Context) { closeWindow() })
	trayMenu.Add("完全退出").OnClick(func(*application.Context) { quitApplication() })
	trayMenu.AddSeparator()
	trayMenu.Add("关于").OnClick(func(*application.Context) { app.Menu.ShowAbout() })
	tray := app.SystemTray.New()
	tray.SetIcon(appicon.PNG)
	tray.SetTooltip("DSH Desktop")
	tray.SetMenu(trayMenu)
	tray.OnClick(showWindow)
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		showWindow()
	})

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		appStarted.Store(true)
		logger.Printf("desktop window started")
		if quitting.Load() {
			app.Quit()
			return
		}
		if startupConsoleOwned {
			hideStartupConsole()
		}
		showWindow()
	})

	app.OnShutdown(func() {
		quitting.Store(true)
		if saveErr := store.save(); saveErr != nil {
			logger.Printf("cannot save window state during shutdown: %v", saveErr)
		}
		if process := backendState.take(); process != nil {
			process.Stop(logger)
		}
	})

	if err = app.Run(); err != nil {
		return fmt.Errorf("desktop window failed: %w", err)
	}
	logger.Printf("application stopped")
	smokeFailureMu.Lock()
	failure := smokeFailure
	smokeFailureMu.Unlock()
	if failure != nil {
		return failure
	}
	return nil
}

func pageReloadKeyBindings() map[string]func(application.Window) {
	reload := func(window application.Window) { window.Reload() }
	return map[string]func(application.Window){
		"CmdOrCtrl+R": reload,
		"F5":          reload,
	}
}

func findBunx() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DSH_BUNX_PATH")); configured != "" {
		path, err := filepath.Abs(configured)
		if err == nil && isExecutableFile(path) {
			return path, nil
		}
	}
	if path, err := exec.LookPath("bunx"); err == nil {
		return filepath.Abs(path)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(home, ".bun", "bin", executableName("bunx")),
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			filepath.Join("/opt/homebrew/bin", executableName("bunx")),
			filepath.Join("/usr/local/bin", executableName("bunx")),
		)
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

func dshEnvironment(base []string) ([]string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return base, ""
	}
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	dshHome := filepath.Join(configHome, "dsh")
	if info, err := os.Stat(dshHome); err != nil || !info.IsDir() {
		return base, ""
	}
	return setEnvironment(base, "DSH_HOME", dshHome), dshHome
}

func setEnvironment(environment []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		comparison := item
		if runtime.GOOS == "windows" {
			comparison = strings.ToUpper(item)
		}
		if strings.HasPrefix(comparison, prefix) {
			continue
		}
		result = append(result, item)
	}
	return append(result, key+"="+value)
}

func environmentValue(environment []string, key string) string {
	for _, item := range environment {
		name, value, found := strings.Cut(item, "=")
		if found && strings.EqualFold(name, key) {
			return value
		}
	}
	return ""
}

func executableSearchPaths(bunxPath string) []string {
	paths := []string{filepath.Dir(bunxPath)}
	home := ""
	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths,
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".local", "share", "fnm", "aliases", "default", "bin"),
			filepath.Join(home, ".nvm", "current", "bin"),
		)
	}
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, "/opt/homebrew/bin", "/usr/local/bin")
	case "linux":
		if homebrewPrefix := strings.TrimSpace(os.Getenv("HOMEBREW_PREFIX")); homebrewPrefix != "" {
			paths = append(paths, filepath.Join(homebrewPrefix, "bin"))
		}
		if home != "" {
			paths = append(paths, filepath.Join(home, ".linuxbrew", "bin"))
		}
		paths = append(paths,
			"/home/linuxbrew/.linuxbrew/bin",
			"/usr/local/bin",
			"/usr/bin",
			"/snap/bin",
		)
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root != "" {
				paths = append(paths, filepath.Join(root, "nodejs"))
			}
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			paths = append(paths, filepath.Join(localAppData, "Programs", "nodejs"))
		}
	}
	return paths
}

func prependExecutablePaths(environment []string, paths ...string) []string {
	existing := filepath.SplitList(environmentValue(environment, "PATH"))
	combined := make([]string, 0, len(paths)+len(existing))
	seen := make(map[string]struct{}, len(paths)+len(existing))
	for _, path := range append(paths, existing...) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		key := filepath.Clean(path)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		combined = append(combined, path)
	}
	return setEnvironment(environment, "PATH", strings.Join(combined, string(os.PathListSeparator)))
}

func dshWorkspace() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DSH_WORKSPACE")); configured != "" {
		path, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("invalid DSH_WORKSPACE: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("DSH_WORKSPACE is not a directory: %s", path)
		}
		return path, nil
	}
	return os.UserHomeDir()
}

func startDSH(bunxPath, workspace string, environment []string, output io.Writer) (*managedProcess, error) {
	ctx := context.Background()
	cmd := newBunxCommand(ctx, bunxPath, dshPackage, "web", "--no-open")
	cmd.Dir = workspace
	cmd.Env = environment
	cmd.Stdout = output
	cmd.Stderr = output
	configureChildProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	process := &managedProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		process.mu.Lock()
		process.waitErr = err
		process.mu.Unlock()
		close(process.done)
	}()
	return process, nil
}

func (process *managedProcess) waitError() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.waitErr
}

func (process *managedProcess) Stop(logger *log.Logger) {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return
	}
	process.stopOnce.Do(func() {
		select {
		case <-process.done:
			return
		default:
		}
		logger.Printf("stopping DSH process tree (pid %d)", process.cmd.Process.Pid)
		_ = terminateProcessTree(process.cmd.Process.Pid, false)
		select {
		case <-process.done:
		case <-time.After(processStopTimeout):
			logger.Printf("forcing DSH process tree to stop")
			_ = terminateProcessTree(process.cmd.Process.Pid, true)
			select {
			case <-process.done:
			case <-time.After(processStopTimeout):
				logger.Printf("DSH process did not report an exit after forced termination")
			}
		}
	})
}

func waitForDSH(process *managedProcess, timeout time.Duration) error {
	return waitForDSHWithProbe(process, timeout, readinessInterval, readinessStability, isDSHReady)
}

func waitForDSHWithProbe(
	process *managedProcess,
	timeout time.Duration,
	interval time.Duration,
	stability time.Duration,
	probe func() bool,
) error {
	var processDone <-chan struct{}
	if process != nil {
		processDone = process.done
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var readySince time.Time

	for {
		if probe() {
			if readySince.IsZero() {
				readySince = time.Now()
			}
			if time.Since(readySince) >= stability {
				return nil
			}
		} else {
			readySince = time.Time{}
		}
		select {
		case <-processDone:
			if err := process.waitError(); err != nil {
				return fmt.Errorf("DSH 在服务就绪前退出：%w", err)
			}
			return errors.New("DSH 在服务就绪前退出")
		case <-deadline.C:
			return fmt.Errorf("等待 DSH 启动超时（%s）", timeout)
		case <-ticker.C:
		}
	}
}

func isDSHReady() bool {
	client := &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	response, err := client.Get(dshURL)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return true
}

func startTimeout() time.Duration {
	return durationFromSeconds("DSH_START_TIMEOUT_SECONDS", defaultStartWait)
}

func smokeTestEnabled() bool {
	for _, argument := range os.Args[1:] {
		if argument == "--smoke-test" {
			return true
		}
	}
	return false
}

func smokeTestDuration() time.Duration {
	return durationFromSeconds("DSH_SMOKE_TEST_SECONDS", 8*time.Second)
}

func durationFromSeconds(name string, fallback time.Duration) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || seconds < 1 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func newStateStore(path string, logger *log.Logger) *stateStore {
	store := &stateStore{path: path, state: defaultWindowState()}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Printf("cannot read window state: %v", err)
		}
		return store
	}
	var saved windowState
	if err = json.Unmarshal(data, &saved); err != nil {
		logger.Printf("cannot parse window state: %v", err)
		return store
	}
	store.state = normaliseWindowState(saved)
	return store
}

func defaultWindowState() windowState {
	return windowState{Width: defaultWidth, Height: defaultHeight}
}

func normaliseWindowState(state windowState) windowState {
	if state.Width < minimumWidth || state.Width > 10000 {
		state.Width = defaultWidth
	}
	if state.Height < minimumHeight || state.Height > 10000 {
		state.Height = defaultHeight
	}
	if state.X < -100000 || state.X > 100000 || state.Y < -100000 || state.Y > 100000 {
		state.X = 0
		state.Y = 0
		state.HasPosition = false
	}
	return state
}

func (store *stateStore) snapshot() windowState {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.state
}

func (store *stateStore) update(change func(*windowState)) {
	store.mu.Lock()
	defer store.mu.Unlock()
	change(&store.state)
	store.state = normaliseWindowState(store.state)
}

func (store *stateStore) save() error {
	state := store.snapshot()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(store.path, append(data, '\n'), 0o600)
}

func windowStatePath() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, appName, "window-state.json"), nil
}

func newLauncherLogger() (*log.Logger, func(), error) {
	path, err := launcherLogPath()
	if err != nil {
		return nil, nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	writers := []io.Writer{file}
	if _, statErr := os.Stdout.Stat(); statErr == nil {
		writers = append(writers, os.Stdout)
	}
	logger := log.New(io.MultiWriter(writers...), "", log.Ldate|log.Ltime|log.Lmicroseconds)
	return logger, func() { _ = file.Close() }, nil
}

func launcherLogPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DSH_LAUNCHER_LOG")); configured != "" {
		return filepath.Abs(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs", appName, "launcher.log"), nil
	case "windows":
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, appName, "launcher.log"), nil
	default:
		base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "dsh-desktop", "launcher.log"), nil
	}
}
