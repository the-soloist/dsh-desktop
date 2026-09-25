package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/the-soloist/dsh-desktop/internal/backend"
	"github.com/the-soloist/dsh-desktop/internal/dshenv"
	"github.com/the-soloist/dsh-desktop/internal/npmregistry"
)

func (controller *controller) startService(restart bool) {
	if restart {
		if !controller.service.beginRestart() {
			return
		}
	} else if !controller.service.beginInitial() {
		return
	}
	go controller.runServiceAction(restart)
}

func (controller *controller) runServiceAction(restart bool) {
	if controller.quitting.Load() {
		return
	}

	controller.setStartupStatus(startupPreparing, "正在检查运行环境", "正在读取环境并查找 bunx、npx 与 Node.js…")
	runtimeEnvironment, err := dshenv.Resolve(os.Environ())
	if runtimeEnvironment.DSHHome != "" {
		if profileErr := controller.profiles.Refresh(runtimeEnvironment.DSHHome); profileErr != nil {
			controller.showStartupFailure("无法读取 Profile 列表。", profileErr)
			return
		}
		controller.refreshProfileMenu()
	}
	if runtimeEnvironment.ShellError != nil {
		controller.logger.Printf("[environment] shell import skipped: %v", runtimeEnvironment.ShellError)
	} else if runtimeEnvironment.Shell.Shell != "" && len(runtimeEnvironment.Shell.Sources) > 0 {
		controller.logger.Printf(
			"[environment] imported %d variables from %s via %s",
			len(runtimeEnvironment.Shell.Variables),
			strings.Join(runtimeEnvironment.Shell.Sources, ", "),
			runtimeEnvironment.Shell.Shell,
		)
	}
	if err != nil {
		switch {
		case errors.Is(err, dshenv.ErrPackageRunnerNotFound):
			controller.showStartupFailure("未找到 bunx 或 npx。请安装 Bun 或 Node.js，并配置工具路径、XDG 路径或 PATH。", err)
		case errors.Is(err, dshenv.ErrNodeNotFound):
			controller.showStartupFailure("未找到 Node.js。请配置 DSH_NODE_PATH、Node 版本管理器、XDG 路径或 PATH。", err)
		default:
			controller.showStartupFailure("无法确定 DSH 工作目录。", err)
		}
		return
	}
	controller.logger.Printf(
		"[environment] runner=%s (%s), node=%s",
		runtimeEnvironment.Runner.Name,
		runtimeEnvironment.Runner.Path,
		runtimeEnvironment.NodePath,
	)
	if runtimeEnvironment.DSHHome != "" {
		controller.logger.Printf("[environment] DSH_HOME=%s", runtimeEnvironment.DSHHome)
	}
	selectedProfile := controller.profiles.Snapshot().Selected
	if err := controller.profiles.Select(selectedProfile); err != nil {
		controller.showStartupFailure("无法使用所选 Profile，请从托盘选择其他配置。", err)
		return
	}
	controller.logger.Printf("[profile] selected=%s", selectedProfile)
	if runtimeEnvironment.Runner.Name == dshenv.RunnerBunx {
		controller.logger.Printf(
			"[environment] TMP=%s TEMP=%s",
			dshenv.EnvironmentValue(runtimeEnvironment.Environment, "TMP"),
			dshenv.EnvironmentValue(runtimeEnvironment.Environment, "TEMP"),
		)
	}
	controller.setStartupStatus(startupVersion, "正在获取 DSH 版本", "正在从 npm registry 查询最新版本…")
	registryClient, err := npmregistry.NewClient(runtimeEnvironment.RegistryURL, nil)
	if err != nil {
		controller.showStartupFailure("npm registry 配置无效。", err)
		return
	}
	versionContext, cancelVersionLookup := context.WithTimeout(controller.serviceContext, 15*time.Second)
	latestVersion, err := registryClient.LatestVersion(versionContext, controller.metadata.DSHPackage)
	cancelVersionLookup()
	if err != nil {
		controller.showStartupFailure("无法获取 DSH 版本，请检查网络或 registry 配置。", err)
		return
	}
	if controller.quitting.Load() {
		return
	}
	packageReference := npmregistry.ExactReference(controller.metadata.DSHPackage, latestVersion)
	controller.logger.Printf("[registry] latest package: %s", packageReference)
	// Validate environment, profile and package version before stopping the
	// previous managed process. Switching never reuses an unidentified service.
	if !controller.prepareService(restart) {
		return
	}
	controller.launchDSH(
		runtimeEnvironment.Runner,
		packageReference,
		runtimeEnvironment.Workspace,
		runtimeEnvironment.Environment,
	)
}

func (controller *controller) launchDSH(runner dshenv.PackageRunner, packageReference, workspace string, environment []string) {
	address := controller.backend.URL()
	port, err := portFromURL(address)
	if err != nil {
		controller.showStartupFailure("无法解析 DSH 端口。", err)
		return
	}
	launch := backend.Launch{
		RunnerPath: runner.Path, PackageReference: packageReference,
		Workspace: workspace, Environment: environment,
		Profile: controller.profiles.Snapshot().Selected, Port: port,
	}
	command, err := launch.DisplayCommand(runner.Name)
	if err != nil {
		controller.showStartupFailure("DSH 启动参数无效。", err)
		return
	}
	controller.setStartupCommand("正在启动 DSH", command)
	controller.logger.Printf("[dsh] working directory: %s", workspace)
	summaryLines := make(chan string, 32)
	webURLs := make(chan string, 1)
	output := newStartupOutputRecorder(controller.logger, func(line string) {
		if webURL, ok := dshWebURL(line, address); ok && hasDSHAuthenticationToken(webURL) {
			select {
			case webURLs <- webURL:
			default:
			}
		}
		select {
		case summaryLines <- line:
		default:
		}
	})
	process, err := controller.backend.Start(context.Background(), launch, output)
	if err != nil {
		if controller.quitting.Load() || errors.Is(err, backend.ErrClosed) {
			return
		}
		controller.showStartupFailure("无法启动 DSH。", err)
		return
	}
	if controller.quitting.Load() {
		controller.stopProcess(process)
		return
	}

	controller.setStartupStatus(startupLaunching, "正在等待 DSH", "DSH 进程已启动，正在等待服务和插件完成初始化…")
	summaryDone := make(chan struct{})
	go controller.consumeOutputSummary(process, summaryLines, summaryDone)
	waitErr := controller.backend.WaitForReady(controller.serviceContext, process, startTimeout())
	output.Flush()
	close(summaryDone)
	if waitErr != nil {
		if stopErr := controller.backend.StopIfCurrent(process); stopErr != nil {
			waitErr = errors.Join(waitErr, stopErr)
		}
		if controller.quitting.Load() {
			return
		}
		if recentOutput := output.recentOutput(); recentOutput != "" {
			waitErr = fmt.Errorf("%w\n\n最近的 DSH 输出：\n%s", waitErr, recentOutput)
		}
		controller.showStartupFailure("DSH 未能完成启动。", waitErr)
		return
	}
	if controller.quitting.Load() {
		controller.stopProcess(process)
		return
	}
	navigationURL := address
	if url, ok := pendingDSHWebURL(webURLs); ok {
		navigationURL = url
	}
	probeStatus := controller.backend.Probe(context.Background())
	if probeStatus == backend.ProbeAuthenticationRequired && !hasDSHAuthenticationToken(navigationURL) {
		controller.setStartupStatus(startupConnecting, "正在等待认证地址", "DSH 已要求认证，正在等待 dsh web 输出认证地址…")
		if url, ok := waitForDSHWebURL(webURLs, process.Done(), authenticationURLWait); ok {
			navigationURL = url
		}
	}
	if probeStatus == backend.ProbeAuthenticationRequired && !hasDSHAuthenticationToken(navigationURL) {
		waitErr = errors.New("DSH 已要求认证，但启动输出中没有可用的认证地址")
		if stopErr := controller.backend.StopIfCurrent(process); stopErr != nil {
			waitErr = errors.Join(waitErr, stopErr)
		}
		controller.showStartupFailure("无法建立 DSH 认证会话。", waitErr)
		return
	}
	var authenticationCookie *http.Cookie
	if probeStatus == backend.ProbeAuthenticationRequired {
		controller.setStartupStatus(startupConnecting, "正在获取认证 Cookie", "正在通过本地网络请求交换认证地址并验证会话 Cookie…")
		cookieContext, cancelCookieExchange := context.WithTimeout(controller.serviceContext, 15*time.Second)
		authenticationCookie, waitErr = exchangeDSHAuthenticationCookie(cookieContext, navigationURL, address)
		cancelCookieExchange()
		if waitErr != nil {
			if stopErr := controller.backend.StopIfCurrent(process); stopErr != nil {
				waitErr = errors.Join(waitErr, stopErr)
			}
			controller.showStartupFailure("无法获取 DSH 认证 Cookie。", waitErr)
			return
		}
		controller.logger.Printf("[dsh] authentication cookie acquired via network")
	}
	controller.showDSH("服务已就绪，正在建立 WebView 会话…", authenticationCookie)
	controller.monitorBackend(process)
}

const authenticationURLWait = 15 * time.Second

func pendingDSHWebURL(urls <-chan string) (string, bool) {
	select {
	case url := <-urls:
		if hasDSHAuthenticationToken(url) {
			return url, true
		}
	default:
	}
	return "", false
}

func waitForDSHWebURL(urls <-chan string, done <-chan struct{}, timeout time.Duration) (string, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case url := <-urls:
			if hasDSHAuthenticationToken(url) {
				return url, true
			}
		case <-done:
			return pendingDSHWebURL(urls)
		case <-timer.C:
			return pendingDSHWebURL(urls)
		}
	}
}

func (controller *controller) consumeOutputSummary(process *backend.Process, lines <-chan string, done <-chan struct{}) {
	reported := make(map[string]struct{})
	for {
		select {
		case line := <-lines:
			key, status, detail, ok := dshOutputSummary(line)
			if !ok {
				continue
			}
			if _, exists := reported[key]; exists {
				continue
			}
			reported[key] = struct{}{}
			// Process output adds diagnostics without changing the active phase.
			controller.setStartupStatus("", status, detail)
		case <-done:
			return
		case <-process.Done():
			return
		}
	}
}

func (controller *controller) monitorBackend(process *backend.Process) {
	go func() {
		<-process.Done()
		controller.onBackendExit(process)
	}()
}

func (controller *controller) onBackendExit(process *backend.Process) {
	controller.actionMu.Lock()
	defer controller.actionMu.Unlock()
	if !controller.backend.ClearIfCurrent(process) || controller.quitting.Load() {
		return
	}
	controller.profiles.ClearActive()
	if !controller.service.markStopped() {
		return
	}
	controller.closeAuthenticationProxy()
	message := "DSH 服务已停止，请点击重试重新启动。"
	if processErr := process.WaitError(); processErr != nil {
		controller.logger.Printf("[dsh] process exited: %v", processErr)
		message += "\n\n" + processErr.Error()
		controller.recordSmokeFailure(fmt.Errorf("DSH process exited: %w", processErr))
	} else {
		controller.logger.Printf("[dsh] process exited")
		controller.recordSmokeFailure(errors.New("DSH process exited"))
	}
	controller.refreshProfileMenu()
	controller.logger.Printf("[startup] DSH 已停止 — %s", strings.ReplaceAll(message, "\n", " "))
	controller.startup.reset(startupStopped, "DSH 已停止", message)
	controller.requestStartupPage(startupIntentNone)
	controller.scheduleSmokeFailureExit()
}

func (controller *controller) stopProcess(process *backend.Process) {
	if err := controller.backend.StopIfCurrent(process); err != nil {
		controller.logger.Printf("[dsh] cannot stop process: %v", err)
		controller.recordSmokeFailure(err)
	}
}

func (controller *controller) setStartupStatus(phase startupPhase, status, detail string) {
	controller.logger.Printf("[startup] %s — %s", status, strings.ReplaceAll(detail, "\n", " "))
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.append(phase, status, detail))
}

func (controller *controller) setStartupCommand(status, command string) {
	controller.logger.Printf("[startup] %s — %s", status, command)
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.appendCommand(status, command))
}

func (controller *controller) showStartupFailure(summary string, failure error) {
	detail := summary
	if failure != nil {
		detail += "\n\n" + failure.Error()
	}
	controller.service.set(serviceFailed)
	controller.refreshProfileMenu()
	controller.logger.Printf("[startup] DSH 启动失败 — %s", strings.ReplaceAll(redactSensitiveOutput(detail), "\n", " "))
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.fail(summary, detail))
	controller.window.show()
	controller.recordSmokeFailure(errors.New(detail))
	controller.scheduleSmokeFailureExit()
}

func (controller *controller) showDSH(message string, authenticationCookie *http.Cookie) {
	controller.logger.Printf("[dsh] ready: %s", controller.backend.URL())
	controller.navigationMu.Lock()
	controller.navigationGeneration++
	generation := controller.navigationGeneration
	controller.navigationMu.Unlock()
	controller.setStartupStatus(startupConnecting, "DSH 已就绪", message)
	controller.navigateToDSH(authenticationCookie, generation)
}

func (controller *controller) navigateToDSH(authenticationCookie *http.Cookie, generation uint64) {
	if !controller.navigationIsCurrent(generation) {
		return
	}
	if authenticationCookie == nil {
		controller.closeAuthenticationProxy()
		controller.logger.Printf("[dsh] opening DSH URL: %s", controller.backend.URL())
		controller.window.window.SetURL(controller.backend.URL())
		controller.markProfileReady()
		controller.window.show()
		controller.scheduleSmokeSuccess()
		return
	}

	controller.setStartupStatus(startupConnecting, "正在建立认证连接", "正在创建本地代理，为 WebView 请求注入已验证的 Cookie…")
	proxy, err := newDSHAuthenticationProxy(controller.backend.URL(), authenticationCookie)
	if err != nil {
		controller.showStartupFailure("无法建立 DSH WebView 会话。", err)
		return
	}
	if !controller.navigationIsCurrent(generation) {
		_ = proxy.Close()
		return
	}
	controller.replaceAuthenticationProxy(proxy)
	controller.setStartupStatus(startupConnecting, "认证连接已就绪", "Cookie 已验证，认证代理已就绪，正在加载 DSH 页面…")
	controller.logger.Printf("[dsh] opening authenticated WebView proxy: %s", proxy.URL())
	controller.window.window.SetURL(proxy.URL())
	controller.markProfileReady()
	controller.window.show()
	controller.scheduleSmokeSuccess()
}

func (controller *controller) navigationIsCurrent(generation uint64) bool {
	if controller.quitting.Load() {
		return false
	}
	controller.navigationMu.Lock()
	defer controller.navigationMu.Unlock()
	return controller.navigationGeneration == generation
}
