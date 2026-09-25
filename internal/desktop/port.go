package desktop

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/the-soloist/dsh-desktop/internal/backend"
)

func portFromURL(raw string) (int, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port in %s", raw)
	}
	return port, nil
}

func loopbackURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (controller *controller) prepareService(restart bool) bool {
	ctx := controller.serviceContext
	if restart {
		controller.logger.Printf("[dsh] restart requested")
		if err := controller.backend.StopCurrent(); err != nil {
			controller.showStartupFailure("无法停止现有 DSH 进程。", err)
			return false
		}
		controller.profiles.ClearActive()
	}
	preferredPort, err := portFromURL(controller.backend.URL())
	if err != nil {
		controller.showStartupFailure("无法解析 DSH 端口。", err)
		return false
	}
	controller.setStartupStatus(startupPreparing, "正在检查已有 DSH", "正在检查其他 DSH 进程，随后检查可用端口…")
	choose := controller.confirmExternalDSH
	if restart {
		// Switching/retrying only owns our child. Other profiles and terminal
		// sessions remain alive, even when they occupy the preferred port.
		choose = func(context.Context, []backend.DSHProcess) (externalDSHChoice, error) {
			return externalDSHOtherPort, nil
		}
	}
	preparation := newLaunchPreparation(choose)
	plan, err := preparation.prepare(ctx, preferredPort)
	if err != nil {
		if ctx.Err() == nil {
			controller.showStartupFailure("无法准备 DSH 启动。", err)
		}
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	controller.backend.SetURL(loopbackURL(plan.port))
	controller.setStartupStatus(startupPreparing, "启动端口已确定", "将使用 "+controller.backend.URL()+" 启动 DSH。")
	return true
}
