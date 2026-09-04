package desktop

import (
	"context"
	"errors"
	"fmt"
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
			controller.showDSH("正在加载现有 DSH 服务…", controller.metadata.DSHURL)
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
	if controller.backend.Probe(context.Background()) == backend.ProbeAuthenticationRequired && !hasDSHAuthenticationToken(navigationURL) {
		waitErr = errors.New("DSH 已要求认证，但启动输出中没有可用的认证地址")
		if stopErr := controller.backend.StopIfCurrent(process); stopErr != nil {
			waitErr = errors.Join(waitErr, stopErr)
		}
		controller.showStartupFailure("无法建立 DSH 认证会话。", waitErr)
		return
	}
	controller.monitorBackend(process)
	controller.showDSH("认证完成，正在加载界面…", navigationURL)
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

func (controller *controller) showDSH(message, navigationURL string) {
	controller.service.set(serviceReady)
	controller.logger.Printf("[dsh] ready: %s", controller.metadata.DSHURL)
	controller.logger.Printf("[startup] DSH 已就绪 — %s", message)
	controller.navigationMu.Lock()
	controller.navigationURL = navigationURL
	controller.navigationGeneration++
	controller.navigationMu.Unlock()
	controller.pendingNavigation.Store(true)
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.append("DSH 已就绪", message, false, true))
	time.AfterFunc(5*time.Second, controller.navigateToDSH)
}

func (controller *controller) navigateToDSH() {
	if controller.quitting.Load() || !controller.pendingNavigation.CompareAndSwap(true, false) {
		return
	}
	controller.navigationMu.Lock()
	navigationURL := controller.navigationURL
	navigationGeneration := controller.navigationGeneration
	controller.navigationURL = controller.metadata.DSHURL
	controller.navigationMu.Unlock()
	if !hasDSHAuthenticationToken(navigationURL) {
		if !controller.navigationIsCurrent(navigationGeneration) {
			return
		}
		controller.logger.Printf("[dsh] opening DSH URL: %s", navigationURL)
		controller.window.window.SetURL(navigationURL)
		controller.window.show()
		controller.scheduleSmokeSuccess()
		return
	}

	// WKWebView starts on the wails:// origin. DSH uses a SameSite=Strict
	// session cookie, so prime the DSH origin before opening the launch token;
	// otherwise the 303 token exchange can redirect to / without its new cookie.
	// Keep the temporary 401 response hidden from the user while doing this.
	controller.window.window.Hide()
	controller.logger.Printf("[dsh] priming WebView origin: %s", controller.metadata.DSHURL)
	controller.window.window.SetURL(controller.metadata.DSHURL)
	time.AfterFunc(500*time.Millisecond, func() {
		if !controller.navigationIsCurrent(navigationGeneration) {
			return
		}
		controller.logger.Printf("[dsh] opening authenticated URL: %s", redactSensitiveOutput(navigationURL))
		controller.window.window.SetURL(navigationURL)
		time.AfterFunc(500*time.Millisecond, func() {
			if !controller.navigationIsCurrent(navigationGeneration) {
				return
			}
			controller.window.show()
			controller.scheduleSmokeSuccess()
		})
	})
}

func (controller *controller) navigationIsCurrent(generation uint64) bool {
	if controller.quitting.Load() {
		return false
	}
	controller.navigationMu.Lock()
	defer controller.navigationMu.Unlock()
	return controller.navigationGeneration == generation
}
