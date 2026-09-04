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
	if restart {
		if !controller.prepareRestart() {
			return
		}
	} else if !controller.prepareInitialStart() {
		return
	}
	if controller.quitting.Load() {
		return
	}

	controller.setStartupStatus("正在检查运行环境", "正在读取环境并查找 bunx、npx 与 Node.js…", false)
	runtimeEnvironment, err := dshenv.Resolve(os.Environ())
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
	controller.setStartupStatus("正在获取 DSH 版本", "正在从 npm registry 查询最新版本…", false)
	registryClient, err := npmregistry.NewClient(runtimeEnvironment.RegistryURL, nil)
	if err != nil {
		controller.showStartupFailure("npm registry 配置无效。", err)
		return
	}
	versionContext, cancelVersionLookup := context.WithTimeout(context.Background(), 15*time.Second)
	latestVersion, err := registryClient.LatestVersion(versionContext, controller.metadata.DSHPackage)
	cancelVersionLookup()
	if err != nil {
		controller.showStartupFailure("无法获取 DSH 最新版本。", err)
		return
	}
	if controller.quitting.Load() {
		return
	}
	packageReference := npmregistry.ExactReference(controller.metadata.DSHPackage, latestVersion)
	controller.logger.Printf("[registry] latest package: %s", packageReference)
	controller.launchDSH(
		runtimeEnvironment.Runner,
		packageReference,
		runtimeEnvironment.Workspace,
		runtimeEnvironment.Environment,
	)
}

func (controller *controller) prepareRestart() bool {
	controller.logger.Printf("[dsh] restart requested")
	if controller.backend.HasManagedProcess() {
		if err := controller.backend.StopCurrent(); err != nil {
			controller.showStartupFailure("无法停止现有 DSH 进程。", err)
			return false
		}
		return true
	}
	switch controller.backend.Probe(context.Background()) {
	case backend.ProbeReady:
		message := fmt.Sprintf("当前 DSH 服务由外部进程启动，%s 不会终止它。", controller.metadata.DisplayName)
		showPlatformWarning(controller.metadata.DisplayName, message)
		controller.service.set(serviceReady)
		controller.window.window.SetURL(controller.metadata.DSHURL)
		controller.window.show()
		return false
	case backend.ProbeAuthenticationRequired:
		controller.showStartupFailure("检测到由外部进程启动且需要认证的 DSH。请先退出该进程，然后重试。", nil)
		return false
	case backend.ProbeUnexpected:
		controller.showStartupFailure(controller.metadata.DSHURL+" 已被其他程序占用。", nil)
		return false
	default:
		return true
	}
}

func (controller *controller) prepareInitialStart() bool {
	controller.setStartupStatus("正在检查本地服务", "正在检测 "+controller.metadata.DSHURL+"…", false)
	switch controller.backend.Probe(context.Background()) {
	case backend.ProbeUnexpected:
		controller.showStartupFailure(controller.metadata.DSHURL+" 已被其他程序占用，检测到的页面不是 DSH。", nil)
		return false
	case backend.ProbeReady:
		controller.setStartupStatus("正在连接 DSH", "检测到已有服务，正在确认其稳定性…", false)
		stableWait := readinessStability + 2*readinessInterval
		if err := controller.backend.WaitForReady(context.Background(), nil, stableWait); err == nil {
			controller.logger.Printf("[dsh] reusing service at %s", controller.metadata.DSHURL)
			controller.showDSH("正在加载现有 DSH 服务…", nil)
			return false
		}
		controller.logger.Printf("[dsh] existing service disappeared during readiness check")
	case backend.ProbeAuthenticationRequired:
		controller.showStartupFailure("检测到已运行的 DSH，但它需要启动时生成的认证地址。请先退出该 DSH 进程，然后重试。", nil)
		return false
	}
	return true
}

func (controller *controller) launchDSH(runner dshenv.PackageRunner, packageReference, workspace string, environment []string) {
	command := dshLaunchCommand(runner.Name, packageReference)
	controller.setStartupCommand("正在启动 DSH", command)
	controller.logger.Printf("[dsh] working directory: %s", workspace)
	summaryLines := make(chan string, 32)
	webURLs := make(chan string, 1)
	output := newStartupOutputRecorder(controller.logger, func(line string) {
		if webURL, ok := dshWebURL(line, controller.metadata.DSHURL); ok {
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
	process, err := controller.backend.Start(context.Background(), runner.Path, packageReference, workspace, environment, output)
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

	controller.setStartupStatus("正在等待 DSH", "DSH 进程已启动，正在等待服务和插件完成初始化…", false)
	summaryDone := make(chan struct{})
	go controller.consumeOutputSummary(process, summaryLines, summaryDone)
	waitErr := controller.backend.WaitForReady(context.Background(), process, startTimeout())
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
	navigationURL := controller.metadata.DSHURL
	select {
	case navigationURL = <-webURLs:
	default:
	}
	probeStatus := controller.backend.Probe(context.Background())
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
		controller.setStartupStatus("正在获取认证 Cookie", "正在通过本地网络请求交换 dsh web 输出的 /?token=xxx…，成功后将注入 WebView Cookie。", false)
		cookieContext, cancelCookieExchange := context.WithTimeout(context.Background(), 15*time.Second)
		authenticationCookie, waitErr = exchangeDSHAuthenticationCookie(cookieContext, navigationURL, controller.metadata.DSHURL)
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
	controller.monitorBackend(process)
	controller.showDSH("服务已就绪，正在建立 WebView 会话…", authenticationCookie)
}

func dshLaunchCommand(runnerName, packageReference string) string {
	return fmt.Sprintf("%s %s web --no-open", runnerName, packageReference)
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
			controller.setStartupStatus(status, detail, false)
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
		controller.closeAuthenticationProxy()
		if !controller.backend.ClearIfCurrent(process) || controller.quitting.Load() {
			return
		}
		message := "DSH 服务已停止。请从托盘菜单选择“重启 DSH”。"
		if processErr := process.WaitError(); processErr != nil {
			controller.logger.Printf("[dsh] process exited: %v", processErr)
			message += "\n\n" + processErr.Error()
			controller.recordSmokeFailure(fmt.Errorf("DSH process exited: %w", processErr))
		} else {
			controller.logger.Printf("[dsh] process exited")
			controller.recordSmokeFailure(errors.New("DSH process exited"))
		}
		controller.service.set(serviceStopped)
		controller.logger.Printf("[startup] DSH 已停止 — %s", strings.ReplaceAll(message, "\n", " "))
		controller.startup.reset("DSH 已停止", message, true)
		controller.requestStartupPage(startupIntentNone)
		controller.scheduleSmokeFailureExit()
	}()
}

func (controller *controller) stopProcess(process *backend.Process) {
	if err := controller.backend.StopIfCurrent(process); err != nil {
		controller.logger.Printf("[dsh] cannot stop process: %v", err)
		controller.recordSmokeFailure(err)
	}
}

func (controller *controller) setStartupStatus(status, detail string, failed bool) {
	controller.logger.Printf("[startup] %s — %s", status, strings.ReplaceAll(detail, "\n", " "))
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.append(status, detail, failed, false))
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
	displayDetail := detail + "\n\n请修复问题后从托盘菜单选择“重启 DSH”。"
	controller.logger.Printf("[startup] DSH 启动失败 — %s", strings.ReplaceAll(detail, "\n", " "))
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.append("DSH 启动失败", displayDetail, true, false))
	controller.window.show()
	controller.recordSmokeFailure(errors.New(detail))
	controller.scheduleSmokeFailureExit()
}

func (controller *controller) showDSH(message string, authenticationCookie *http.Cookie) {
	controller.service.set(serviceReady)
	controller.logger.Printf("[dsh] ready: %s", controller.metadata.DSHURL)
	authenticationRequired := authenticationCookie != nil
	if authenticationRequired {
		message = "服务已就绪，正在建立 WebView 会话…"
	}
	controller.logger.Printf("[startup] DSH 已就绪 — %s", message)
	controller.navigationMu.Lock()
	controller.navigationCookie = authenticationCookie
	controller.navigationGeneration++
	controller.navigationMu.Unlock()
	controller.pendingNavigation.Store(true)
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.append("DSH 已就绪", message, false, !authenticationRequired))
	if authenticationRequired {
		controller.setStartupStatus("正在建立 WebView 会话", "准备使用网络请求获得的 Cookie 建立认证连接…", false)
	}
	time.AfterFunc(5*time.Second, controller.navigateToDSH)
}

func (controller *controller) navigateToDSH() {
	if controller.quitting.Load() || !controller.pendingNavigation.CompareAndSwap(true, false) {
		return
	}
	controller.navigationMu.Lock()
	navigationGeneration := controller.navigationGeneration
	authenticationCookie := controller.navigationCookie
	controller.navigationCookie = nil
	controller.navigationMu.Unlock()
	if authenticationCookie == nil {
		if !controller.navigationIsCurrent(navigationGeneration) {
			return
		}
		controller.closeAuthenticationProxy()
		controller.logger.Printf("[dsh] opening DSH URL: %s", controller.metadata.DSHURL)
		controller.window.window.SetURL(controller.metadata.DSHURL)
		controller.window.show()
		controller.scheduleSmokeSuccess()
		return
	}

	controller.setStartupStatus("正在注入 WebView Cookie", "正在把网络请求获得的认证 Cookie 注入 WebView 请求…", false)
	proxy, err := newDSHAuthenticationProxy(controller.metadata.DSHURL, authenticationCookie)
	if err != nil {
		controller.showStartupFailure("无法建立 DSH WebView 会话。", err)
		return
	}
	if !controller.navigationIsCurrent(navigationGeneration) {
		_ = proxy.Close()
		return
	}
	controller.replaceAuthenticationProxy(proxy)
	controller.setStartupStatus("Cookie 已生效", "已通过本地网络请求验证 Cookie，并注入后续 WebView 请求，正在加载 DSH 页面…", false)
	controller.logger.Printf("[dsh] opening authenticated WebView proxy: %s", proxy.URL())
	controller.window.window.SetURL(proxy.URL())
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
