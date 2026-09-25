package desktop

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/the-soloist/dsh-desktop/internal/backend"
)

func (controller *controller) confirmExternalDSH(ctx context.Context, processes []backend.DSHProcess) (externalDSHChoice, error) {
	pids := make([]string, 0, len(processes))
	for _, process := range processes {
		pids = append(pids, strconv.Itoa(process.PID))
	}
	message := fmt.Sprintf("检测到 %d 个已有 DSH 实例（PID：%s）。\n\n是否结束这些实例及其运行中的任务，然后重新启动？\n选择“否”会保留已有实例，继续选择是否启动新实例。", len(processes), strings.Join(pids, ", "))
	return chooseExternalDSH(ctx, message, controller.askDSHQuestion)
}

func chooseExternalDSH(ctx context.Context, message string, ask func(context.Context, string) (bool, error)) (externalDSHChoice, error) {
	stop, err := ask(ctx, message)
	if err != nil {
		return externalDSHCancel, err
	}
	if stop {
		return externalDSHKill, nil
	}
	start, err := ask(ctx, "保留已有 DSH 实例，并在可用端口启动一个新实例？\n\n选择“否”会取消本次启动。")
	if err != nil {
		return externalDSHCancel, err
	}
	if start {
		return externalDSHOtherPort, nil
	}
	return externalDSHCancel, nil
}

func questionButtonLabels(goos string) (string, string) {
	// Wails' Windows MessageBox maps callbacks by these exact labels.
	if goos == "windows" {
		return "Yes", "No"
	}
	return "是", "否"
}

func (controller *controller) askDSHQuestion(ctx context.Context, message string) (bool, error) {
	return controller.askQuestion(ctx, "检测到已有 DSH", message)
}

func (controller *controller) askQuestion(ctx context.Context, title, message string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	chosen := make(chan bool, 1)
	answer := func(value bool) {
		select {
		case chosen <- value:
		default:
		}
	}
	yes, no := questionButtonLabels(runtime.GOOS)
	dialog := controller.app.Dialog.Question().SetTitle(title).SetMessage(message)
	dialog.AttachToWindow(controller.window.window)
	dialog.AddButton(yes).OnClick(func() { answer(true) })
	cancel := dialog.AddButton(no).OnClick(func() { answer(false) })
	dialog.SetDefaultButton(cancel).SetCancelButton(cancel)
	go dialog.Show()
	// Human decisions have no timeout. Shutdown cancels any pending decision.
	select {
	case choice := <-chosen:
		return choice, ctx.Err()
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
