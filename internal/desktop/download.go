package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const downloadMessageType = "dsh-desktop-download"

// downloadInterceptorScript catches the anchor.download pattern used by DSH's
// Session export client. Wails v3 does not expose a native download callback
// consistently across WebKit, WebView2, and WebKitGTK, so the URL is handed to
// the Go side through the platform message bridge instead.
const downloadInterceptorScript = `(function () {
  if (window.__dshDesktopDownloadInterceptor) return;
  window.__dshDesktopDownloadInterceptor = true;

  function send(payload) {
    try {
      if (window.chrome && window.chrome.webview && window.chrome.webview.postMessage) {
        window.chrome.webview.postMessage(payload);
        return true;
      }
      if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.external) {
        window.webkit.messageHandlers.external.postMessage(payload);
        return true;
      }
      if (window._wails && typeof window._wails.invoke === "function") {
        window._wails.invoke(payload);
        return true;
      }
    } catch (_) {}
    return false;
  }

  function intercept(anchor) {
    if (!anchor || !anchor.hasAttribute("download")) return false;
    var href = anchor.href;
    if (!href) return false;
    var destination;
    try {
      destination = new URL(href, window.location.href);
    } catch (_) {
      return false;
    }
    if (destination.origin !== window.location.origin) return false;
    var message = JSON.stringify({
      type: "dsh-desktop-download",
      url: destination.href,
      filename: anchor.download || ""
    });
    return send(message);
  }

  // DSH creates the export anchor without appending it to document, so a
  // document click listener alone cannot observe anchor.click().
  var originalAnchorClick = HTMLAnchorElement.prototype.click;
  HTMLAnchorElement.prototype.click = function () {
    if (intercept(this)) return;
    return originalAnchorClick.call(this);
  };

  document.addEventListener("click", function (event) {
    var target = event.target;
    var anchor = target && typeof target.closest === "function" ? target.closest("a[download]") : null;
    if (intercept(anchor)) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);
})();`

type desktopDownloadRequest struct {
	Type     string `json:"type"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

var errDownloadOriginNotAllowed = errors.New("download request did not originate from the active DSH page")

func (controller *controller) handleRawMessage(window application.Window, message string, originInfo *application.OriginInfo) {
	var request desktopDownloadRequest
	if err := json.Unmarshal([]byte(message), &request); err != nil || request.Type != downloadMessageType {
		return
	}
	if controller.quitting.Load() {
		return
	}
	if err := controller.validateDownloadRequest(window, request, originInfo); err != nil {
		controller.logger.Printf("[download] rejected request: %v", err)
		return
	}

	// Save dialogs are modal and the network response can be large. Serialise
	// requests so two rapid clicks cannot race the native dialog or overwrite a
	// destination selected for another download.
	go func() {
		controller.downloadMu.Lock()
		defer controller.downloadMu.Unlock()
		controller.downloadFile(window, request)
	}()
}

func (controller *controller) validateDownloadRequest(window application.Window, request desktopDownloadRequest, originInfo *application.OriginInfo) error {
	if window == nil || window.Name() != "main" {
		return errDownloadOriginNotAllowed
	}
	destination, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || destination.Scheme != "http" || destination.Host == "" {
		return errDownloadOriginNotAllowed
	}

	controller.proxyMu.Lock()
	proxy := controller.authenticationProxy
	controller.proxyMu.Unlock()
	expectedOrigin := controller.metadata.DSHURL
	if proxy != nil {
		expectedOrigin = proxy.URL()
	}
	if !sameHTTPOrigin(destination.String(), expectedOrigin) {
		return errDownloadOriginNotAllowed
	}
	if originInfo != nil && strings.TrimSpace(originInfo.Origin) != "" && !sameHTTPOrigin(originInfo.Origin, expectedOrigin) {
		return errDownloadOriginNotAllowed
	}
	return nil
}

func sameHTTPOrigin(first, second string) bool {
	left, leftErr := url.Parse(first)
	right, rightErr := url.Parse(second)
	if leftErr != nil || rightErr != nil || left.Scheme == "" || right.Scheme == "" {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func (controller *controller) downloadFile(window application.Window, request desktopDownloadRequest) {
	destinationURL, err := url.Parse(request.URL)
	if err != nil {
		controller.reportDownloadError(window, err)
		return
	}
	filename := downloadFilename(request.Filename, destinationURL.Path)
	destination, err := controller.app.Dialog.SaveFile().
		SetFilename(filename).
		SetMessage("选择保存下载文件的位置").
		AttachToWindow(window).
		PromptForSingleSelection()
	if err != nil {
		controller.reportDownloadError(window, fmt.Errorf("打开保存对话框: %w", err))
		return
	}
	if strings.TrimSpace(destination) == "" {
		controller.logger.Printf("[download] cancelled: %s", filename)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	requestWithContext, err := http.NewRequestWithContext(ctx, http.MethodGet, destinationURL.String(), nil)
	if err != nil {
		controller.reportDownloadError(window, fmt.Errorf("创建下载请求: %w", err))
		return
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(requestWithContext)
	if err != nil {
		controller.reportDownloadError(window, fmt.Errorf("下载文件: %w", err))
		return
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		controller.reportDownloadError(window, fmt.Errorf("下载文件: DSH 返回 HTTP %d", response.StatusCode))
		return
	}
	if err := writeDownloadFile(destination, response.Body); err != nil {
		controller.reportDownloadError(window, err)
		return
	}
	controller.logger.Printf("[download] saved %s", destination)
}

func writeDownloadFile(destination string, source io.Reader) error {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return errors.New("下载文件: 未选择保存位置")
	}
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".dsh-download-*")
	if err != nil {
		return fmt.Errorf("创建临时下载文件: %w", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := io.Copy(temporary, source); err != nil {
		return fmt.Errorf("写入下载文件: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步下载文件: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭下载文件: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		// Windows does not replace an existing file with Rename. The save
		// dialog already asked for overwrite confirmation, so removing that
		// selected destination is safe and keeps the operation cross-platform.
		if !os.IsExist(err) {
			return fmt.Errorf("保存下载文件: %w", err)
		}
		if removeErr := os.Remove(destination); removeErr != nil {
			return fmt.Errorf("保存下载文件: %w", err)
		}
		if retryErr := os.Rename(temporaryName, destination); retryErr != nil {
			return fmt.Errorf("保存下载文件: %w", retryErr)
		}
	}
	removeTemporary = false
	return nil
}

func downloadFilename(filename, requestPath string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = path.Base(requestPath)
	}
	if name == "" || name == "." || name == "/" {
		name = "download"
	}
	var builder strings.Builder
	for _, character := range name {
		switch {
		case character < 0x20, strings.ContainsRune(`<>:"/\\|?*`, character):
			builder.WriteByte('_')
		default:
			builder.WriteRune(character)
		}
	}
	name = strings.Trim(strings.TrimSpace(builder.String()), ".")
	if name == "" || name == "." || name == ".." {
		name = "download"
	}
	if len([]rune(name)) > 180 {
		name = string([]rune(name)[:180])
	}
	return name
}

func (controller *controller) reportDownloadError(window application.Window, err error) {
	controller.logger.Printf("[download] failed: %v", err)
	if controller.quitting.Load() {
		return
	}
	controller.app.Dialog.Error().
		SetTitle("下载失败").
		SetMessage(err.Error()).
		AttachToWindow(window).
		Show()
}
