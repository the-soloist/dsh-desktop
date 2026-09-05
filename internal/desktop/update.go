package desktop

import (
	"context"
	"fmt"
	"time"

	dshdesktop "github.com/the-soloist/dsh-desktop"
	releaseupdate "github.com/the-soloist/dsh-desktop/internal/update"
)

const (
	updateCheckInterval  = 3 * 24 * time.Hour
	updateRequestTimeout = 15 * time.Second
)

func (controller *controller) startUpdateChecks() {
	controller.updateLifecycleMu.Lock()
	if controller.updateDone != nil {
		controller.updateLifecycleMu.Unlock()
		return
	}
	contextValue, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	controller.updateCancel = cancel
	controller.updateDone = done
	controller.updateLifecycleMu.Unlock()

	go func() {
		defer close(done)
		controller.runScheduledUpdateCheck(contextValue, false)
		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-contextValue.Done():
				return
			case <-ticker.C:
				controller.runScheduledUpdateCheck(contextValue, false)
			}
		}
	}()
}

func (controller *controller) stopUpdateChecks() {
	controller.updateLifecycleMu.Lock()
	cancel := controller.updateCancel
	done := controller.updateDone
	controller.updateCancel = nil
	controller.updateDone = nil
	controller.updateLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (controller *controller) requestUpdateCheck() {
	if controller.quitting.Load() {
		return
	}
	go controller.runScheduledUpdateCheck(context.Background(), true)
}

func (controller *controller) runScheduledUpdateCheck(parent context.Context, manual bool) {
	if !manual && controller.updateSchedule != nil && !controller.updateSchedule.Due(time.Now()) {
		controller.logger.Printf("[update] automatic check skipped; three-day interval has not elapsed")
		return
	}
	if controller.checkForUpdates(parent, manual) && controller.updateSchedule != nil {
		if err := controller.updateSchedule.Mark(time.Now()); err != nil {
			controller.logger.Printf("[update] cannot persist check schedule: %v", err)
		}
	}
}

func (controller *controller) checkForUpdates(parent context.Context, manual bool) bool {
	if !controller.updateCheckMu.TryLock() {
		if manual {
			controller.logger.Printf("[update] check already in progress")
		}
		return false
	}
	defer controller.updateCheckMu.Unlock()
	if controller.updateClient == nil {
		controller.reportUpdateResult(manual, nil, fmt.Errorf("GitHub Releases client is unavailable"))
		return true
	}
	version, err := dshdesktop.CurrentVersion()
	if err != nil {
		controller.reportUpdateResult(manual, nil, fmt.Errorf("read current application version: %w", err))
		return true
	}
	controller.logger.Printf("[update] checking GitHub Releases (current=%s)", version)
	requestContext, cancel := context.WithTimeout(parent, updateRequestTimeout)
	release, err := controller.updateClient.Check(requestContext, version.String())
	cancel()
	if controller.quitting.Load() {
		return true
	}
	controller.reportUpdateResult(manual, release, err)
	return true
}

func (controller *controller) reportUpdateResult(manual bool, release *releaseupdate.Release, err error) {
	if err != nil {
		controller.logger.Printf("[update] check failed: %v", err)
		if manual {
			controller.showUpdateError(err)
		}
		return
	}
	currentVersion, versionErr := dshdesktop.CurrentVersion()
	if versionErr != nil {
		controller.logger.Printf("[update] cannot read current version: %v", versionErr)
		return
	}
	if release == nil {
		controller.logger.Printf("[update] no newer compatible release (current=%s)", currentVersion)
		if manual {
			controller.showUpdateInfo(fmt.Sprintf("当前已是最新版本 v%s。", currentVersion))
		}
		return
	}
	controller.logger.Printf("[update] update available: %s (%s)", release.Version, release.AssetName)
	controller.showUpdateAvailable(release)
}

func (controller *controller) showUpdateAvailable(release *releaseupdate.Release) {
	controller.updateDialogMu.Lock()
	defer controller.updateDialogMu.Unlock()
	if controller.quitting.Load() {
		return
	}
	name := release.Name
	if name == "" {
		name = "DSH Desktop"
	}
	message := fmt.Sprintf("发现 %s 新版本 v%s。\n\n当前发布文件：%s\n\n打开 GitHub Releases 页面下载并安装新版本。", name, release.Version, release.AssetName)
	dialog := controller.app.Dialog.Info().SetTitle("发现新版本").SetMessage(message)
	open := dialog.AddButton("打开下载页面").OnClick(func() {
		if err := controller.app.Browser.OpenURL(release.HTMLURL); err != nil {
			controller.logger.Printf("[update] cannot open release page: %v", err)
		}
	})
	later := dialog.AddButton("稍后")
	dialog.SetDefaultButton(open).SetCancelButton(later)
	dialog.Show()
}

func (controller *controller) showUpdateInfo(message string) {
	controller.updateDialogMu.Lock()
	defer controller.updateDialogMu.Unlock()
	if controller.quitting.Load() {
		return
	}
	dialog := controller.app.Dialog.Info().SetTitle("检查更新").SetMessage(message)
	ok := dialog.AddButton("确定")
	dialog.SetDefaultButton(ok).SetCancelButton(ok)
	dialog.Show()
}

func (controller *controller) showUpdateError(err error) {
	controller.updateDialogMu.Lock()
	defer controller.updateDialogMu.Unlock()
	if controller.quitting.Load() {
		return
	}
	dialog := controller.app.Dialog.Error().SetTitle("检查更新失败").SetMessage(err.Error())
	ok := dialog.AddButton("确定")
	dialog.SetDefaultButton(ok).SetCancelButton(ok)
	dialog.Show()
}
