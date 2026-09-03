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

	controller.setStartupStatus("正在检查运行环境", "正在查找 bunx 与 Node.js…", false)
	bunxPath, err := dshenv.FindBunx()
	if err != nil {
		controller.showStartupFailure("未找到 bunx。请先安装 Bun，并确保 bunx 已加入 PATH。", err)
		return
	}
	controller.logger.Printf("using bunx: %s", bunxPath)

	environment, dshHome := dshenv.BuildEnvironment(os.Environ(), bunxPath)
	if dshHome != "" {
		controller.logger.Printf("using DSH_HOME: %s", dshHome)
	}
	controller.logger.Printf("DSH executable search path: %s", dshenv.EnvironmentValue(environment, "PATH"))
	if _, err = dshenv.FindNode(environment); err != nil {
		controller.showStartupFailure("未找到 Node.js。当前 DSH 命令需要 Node.js，请安装后重试。", err)
		return
	}
	workspace, err := dshenv.Workspace()
	if err != nil {
		controller.showStartupFailure("无法确定 DSH 工作目录。", err)
		return
	}
	controller.launchDSH(bunxPath, workspace, environment)
}

func (controller *controller) prepareRestart() bool {
	controller.logger.Printf("DSH restart requested")
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
			controller.logger.Printf("reusing service already listening at %s", controller.metadata.DSHURL)
			controller.showDSH("正在加载现有 DSH 服务…")
			return false
		}
		controller.logger.Printf("existing DSH service disappeared during readiness check")
	}
	return true
}

func (controller *controller) launchDSH(bunxPath, workspace string, environment []string) {
	controller.setStartupStatus("正在启动 DSH", "正在后台启动本地 Web 服务…", false)
	controller.logger.Printf("starting DSH in %s", workspace)
	summaryLines := make(chan string, 32)
	output := newStartupOutputRecorder(controller.logger.Writer(), func(line string) {
		select {
		case summaryLines <- line:
		default:
		}
	})
	process, err := controller.backend.Start(context.Background(), bunxPath, workspace, environment, output)
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
	controller.monitorBackend(process)
	controller.showDSH("启动完成，正在加载界面…")
}

func (controller *controller) consumeOutputSummary(process *backend.Process, lines <-chan string, done <-chan struct{}) {
	reported := make(map[string]struct{})
	for {
		select {
		case line := <-lines:
			key, status, detail, ok := dshOutputSummary(line, controller.metadata.DSHURL)
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
			controller.logger.Printf("DSH process exited: %v", processErr)
			message += "\n\n" + processErr.Error()
			controller.recordSmokeFailure(fmt.Errorf("DSH process exited: %w", processErr))
		} else {
			controller.logger.Printf("DSH process exited")
			controller.recordSmokeFailure(errors.New("DSH process exited"))
		}
		controller.service.set(serviceStopped)
		controller.logger.Printf("startup status: DSH 已停止 — %s", strings.ReplaceAll(message, "\n", " "))
		controller.startup.reset("DSH 已停止", message, true)
		controller.requestStartupPage(startupIntentNone)
		controller.scheduleSmokeFailureExit()
	}()
}

func (controller *controller) stopProcess(process *backend.Process) {
	if err := controller.backend.StopIfCurrent(process); err != nil {
		controller.logger.Printf("cannot stop DSH process: %v", err)
		controller.recordSmokeFailure(err)
	}
}

func (controller *controller) setStartupStatus(status, detail string, failed bool) {
	controller.logger.Printf("startup status: %s — %s", status, strings.ReplaceAll(detail, "\n", " "))
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.append(status, detail, failed, false))
}

func (controller *controller) showStartupFailure(summary string, failure error) {
	detail := summary
	if failure != nil {
		detail += "\n\n" + failure.Error()
	}
	controller.service.set(serviceFailed)
	controller.logger.Printf("DSH startup failed: %s", strings.ReplaceAll(detail, "\n", " "))
	controller.setStartupStatus("DSH 启动失败", detail+"\n\n请修复问题后从托盘菜单选择“重启 DSH”。", true)
	controller.window.show()
	controller.recordSmokeFailure(errors.New(detail))
	controller.scheduleSmokeFailureExit()
}

func (controller *controller) showDSH(message string) {
	controller.service.set(serviceReady)
	controller.logger.Printf("DSH is ready at %s", controller.metadata.DSHURL)
	controller.pendingNavigation.Store(true)
	controller.window.window.EmitEvent(startupUpdateEvent, controller.startup.append("DSH 已就绪", message, false, true))
	time.AfterFunc(5*time.Second, controller.navigateToDSH)
}

func (controller *controller) navigateToDSH() {
	if controller.quitting.Load() || !controller.pendingNavigation.CompareAndSwap(true, false) {
		return
	}
	controller.window.window.SetURL(controller.metadata.DSHURL)
	controller.window.show()
	controller.scheduleSmokeSuccess()
}
