package desktop

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/the-soloist/dsh-desktop/internal/dshenv"
	"github.com/the-soloist/dsh-desktop/internal/profile"
)

func profileActionsEnabled(phase servicePhase) bool {
	return phase == serviceReady || phase == serviceFailed || phase == serviceStopped
}

func (controller *controller) populateProfileMenu() {
	state := controller.profiles.Snapshot()
	enabled := profileActionsEnabled(controller.service.current()) && !controller.profileConfirming.Load()
	for _, entry := range state.Entries {
		label := entry.Name
		if entry.Reason != "" {
			label += "（" + entry.Reason + "）"
		} else if entry.Name == state.Selected && entry.Name != state.Active {
			label += "（待启动）"
		}
		controller.profileMenu.AddRadio(label, entry.Name == state.Selected).
			SetEnabled(enabled && entry.Reason == "" && state.Home != "").
			OnClick(func(*application.Context) { controller.requestProfile(entry.Name) })
	}
	controller.profileMenu.AddSeparator()
	controller.profileMenu.Add("刷新列表").SetEnabled(enabled).
		OnClick(func(*application.Context) { controller.refreshProfiles() })
}

func (controller *controller) refreshProfileMenu() {
	if controller.tray == nil || controller.quitting.Load() {
		return
	}
	controller.trayMu.Lock()
	defer controller.trayMu.Unlock()
	application.InvokeSync(func() {
		controller.profileMenu.Destroy()
		controller.populateProfileMenu()
		controller.tray.SetMenu(controller.trayMenu)
	})
}

func (controller *controller) refreshProfiles() {
	controller.actionMu.Lock()
	defer controller.actionMu.Unlock()
	if !profileActionsEnabled(controller.service.current()) || controller.quitting.Load() {
		return
	}
	home := controller.profiles.Snapshot().Home
	if home == "" {
		resolved, _ := dshenv.Resolve(os.Environ())
		home = resolved.DSHHome
	}
	if err := controller.profiles.Refresh(home); err != nil {
		controller.profileWarning(err)
	}
	controller.refreshProfileMenu()
}

func (controller *controller) requestProfile(name string) {
	// Serialise the confirmation and restart with all other switch requests.
	if !controller.actionMu.TryLock() {
		controller.refreshProfileMenu()
		return
	}
	defer controller.actionMu.Unlock()
	defer func() {
		controller.profileConfirming.Store(false)
		controller.refreshProfileMenu()
	}()
	controller.refreshProfileMenu() // Undo the native radio's eager checkmark.
	state := controller.profiles.Snapshot()
	phase := controller.service.current()
	if controller.quitting.Load() || state.Home == "" || !profileActionsEnabled(phase) || (name == state.Active && phase == serviceReady) {
		return
	}
	if entry := profile.Inspect(state.Home, name); entry.Reason != "" {
		controller.profileWarning(fmt.Errorf("无法切换到 %s：%s", name, entry.Reason))
		return
	}
	controller.window.show()
	if controller.backend.HasManagedProcess() {
		controller.profileConfirming.Store(true)
		controller.refreshProfileMenu()
		confirmed, err := controller.askQuestion(controller.serviceContext, "切换 Profile",
			fmt.Sprintf("切换到 Profile %q？\n\n这将重启本应用管理的 DSH 服务，并可能中断正在执行的任务。其他 DSH 实例不会被结束。", name))
		if err != nil || !confirmed {
			return
		}
	}
	previousPhase := controller.service.current()
	if controller.quitting.Load() || !controller.service.scheduleRestart() {
		return
	}
	if err := controller.profiles.Select(name); err != nil {
		// The directory may have disappeared while the confirmation was open.
		controller.service.set(previousPhase)
		controller.profileWarning(err)
		return
	}
	controller.logger.Printf("[profile] switching to %s", name)
	controller.startup.reset(startupPreparing, "正在切换 Profile", "即将启动 Profile："+name)
	controller.requestStartupPage(startupIntentRestart)
}

func (controller *controller) profileWarning(err error) {
	controller.logger.Printf("[profile] %v", err)
	controller.app.Dialog.Warning().SetTitle("切换 Profile").SetMessage(err.Error()).
		AttachToWindow(controller.window.window).Show()
}

func (controller *controller) markProfileReady() {
	if err := controller.profiles.MarkReady(); err != nil {
		controller.logger.Printf("[profile] cannot save selection: %v", err)
	}
	controller.service.set(serviceReady)
	controller.refreshProfileMenu()
}
