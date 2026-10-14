package desktop

import (
	"context"
	"fmt"
	"time"

	dshdesktop "github.com/the-soloist/dsh-desktop"
	releaseupdate "github.com/the-soloist/dsh-desktop/internal/update"
)

const updateRequestTimeout = 15 * time.Second

func (controller *controller) requestUpdateCheck() {
	if controller.quitting.Load() {
		return
	}
	go controller.checkForUpdates(controller.serviceContext)
}

func (controller *controller) checkForUpdates(parent context.Context) {
	if !controller.updateCheckMu.TryLock() {
		controller.logger.Printf("[update] check already in progress")
		return
	}
	defer controller.updateCheckMu.Unlock()
	if controller.updateClient == nil {
		controller.reportUpdateResult(nil, fmt.Errorf("GitHub Releases client is unavailable"))
		return
	}
	version, err := dshdesktop.CurrentVersion()
	if err != nil {
		controller.reportUpdateResult(nil, fmt.Errorf("read current application version: %w", err))
		return
	}
	controller.logger.Printf("[update] checking GitHub Releases (current=%s)", version)
	requestContext, cancel := context.WithTimeout(parent, updateRequestTimeout)
	release, err := controller.updateClient.Check(requestContext, version.String())
	cancel()
	if controller.quitting.Load() {
		return
	}
	controller.reportUpdateResult(release, err)
}

func (controller *controller) reportUpdateResult(release *releaseupdate.Release, err error) {
	if err != nil {
		controller.logger.Printf("[update] check failed: %v", err)
		controller.showUpdateError(err)
		return
	}
	currentVersion, versionErr := dshdesktop.CurrentVersion()
	if versionErr != nil {
		controller.logger.Printf("[update] cannot read current version: %v", versionErr)
		return
	}
	if release == nil {
		controller.logger.Printf("[update] no newer compatible release (current=%s)", currentVersion)
		controller.showUpdateInfo(fmt.Sprintf("当前已是最新版本 v%s。", currentVersion))
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
