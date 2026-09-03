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

	logger.Printf("starting %s on %s/%s", appName, runtime.GOOS, runtime.GOARCH)
	bunxPath, err := findBunx()
	if err != nil {
		return errors.New("未找到 bunx。请先安装 Bun，并确保 bunx 已加入 PATH，然后重新启动 DshDesktop。")
	}
	logger.Printf("using bunx: %s", bunxPath)

	environment, dshHome := dshEnvironment(os.Environ())
	environment = prependExecutablePaths(environment, executableSearchPaths(bunxPath)...)
	if dshHome != "" {
		logger.Printf("using DSH_HOME: %s", dshHome)
	}
	logger.Printf("DSH executable search path: %s", environmentValue(environment, "PATH"))

	var backend *managedProcess
	if isDSHReady() {
		logger.Printf("reusing service already listening at %s", dshURL)
	} else {
		workspace, workspaceErr := dshWorkspace()
		if workspaceErr != nil {
			return workspaceErr
		}
		logger.Printf("starting DSH in %s", workspace)
		backend, err = startDSH(bunxPath, workspace, environment, logger.Writer())
		if err != nil {
			return fmt.Errorf("无法启动 DSH：%w", err)
		}
		defer backend.Stop(logger)

		if err = waitForDSH(backend, startTimeout()); err != nil {
			return err
		}
		logger.Printf("DSH is ready at %s", dshURL)
	}

	statePath, err := windowStatePath()
	if err != nil {
		return fmt.Errorf("cannot resolve the window-state path: %w", err)
	}
	store := newStateStore(statePath, logger)
	initialState := store.snapshot()

	app := application.New(application.Options{
		Name:        appName,
		Description: appDescription,
		Icon:        appicon.PNG,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ProgramName: appName},
	})

	windowOptions := application.WebviewWindowOptions{
		Name:             "main",
		Title:            "DSH Desktop",
		URL:              dshURL,
		Width:            initialState.Width,
		Height:           initialState.Height,
		MinWidth:         minimumWidth,
		MinHeight:        minimumHeight,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(18, 18, 18),
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
	var quitting atomic.Bool
	var appStarted atomic.Bool
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

	trayMenu := app.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(*application.Context) { showWindow() })
	trayMenu.Add("关闭窗口").OnClick(func(*application.Context) { closeWindow() })
	trayMenu.AddSeparator()
	trayMenu.Add("完全退出").OnClick(func(*application.Context) { quitApplication() })
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
		if smokeTestEnabled() {
			duration := smokeTestDuration()
			closeDelay := time.Second
			if duration <= 2*time.Second {
				closeDelay = duration / 2
			}
			logger.Printf("smoke test will close the window after %s and exit after %s", closeDelay, duration)
			time.AfterFunc(closeDelay, window.Close)
			time.AfterFunc(duration, quitApplication)
		}
	})

	app.OnShutdown(func() {
		quitting.Store(true)
		if saveErr := store.save(); saveErr != nil {
			logger.Printf("cannot save window state during shutdown: %v", saveErr)
		}
		if backend != nil {
			backend.Stop(logger)
		}
	})

	if backend != nil {
		go func() {
			<-backend.done
			if backend.waitError() != nil {
				logger.Printf("DSH process exited: %v", backend.waitError())
			}
			quitApplication()
		}()
	}

	if err = app.Run(); err != nil {
		return fmt.Errorf("desktop window failed: %w", err)
	}
	logger.Printf("application stopped")
	return nil
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
		case <-process.done:
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
	logger := log.New(io.MultiWriter(os.Stdout, file), "", log.Ldate|log.Ltime|log.Lmicroseconds)
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
