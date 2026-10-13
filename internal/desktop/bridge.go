package desktop

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed bridge.js
var desktopBridgeScript string

func (controller *controller) handleRawMessage(window application.Window, message string, originInfo *application.OriginInfo) {
	if controller.quitting.Load() {
		return
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(message), &envelope) != nil {
		return
	}
	switch envelope.Type {
	case downloadMessageType:
		controller.handleDownloadMessage(window, message, originInfo)
	case clipboardMessageType:
		controller.handleClipboardMessage(window, message, originInfo, controller.app.Clipboard.SetText)
	}
}

func (controller *controller) activePageOrigin() string {
	controller.proxyMu.Lock()
	defer controller.proxyMu.Unlock()
	if controller.authenticationProxy != nil {
		return controller.authenticationProxy.URL()
	}
	return controller.backend.URL()
}

// Page URLs supplied by JavaScript are not proof of origin. Both the requested
// URL and the platform-reported sender must match the active local DSH origin.
func allowedPageMessageOrigin(window application.Window, pageURL, expectedOrigin string, originInfo *application.OriginInfo) bool {
	if window == nil || window.Name() != "main" || originInfo == nil || strings.TrimSpace(originInfo.Origin) == "" {
		return false
	}
	if !sameHTTPOrigin(pageURL, expectedOrigin) || !sameHTTPOrigin(originInfo.Origin, expectedOrigin) {
		return false
	}
	return originInfo.TopOrigin == "" || sameHTTPOrigin(originInfo.TopOrigin, expectedOrigin)
}

func sameHTTPOrigin(first, second string) bool {
	left, leftErr := url.Parse(first)
	right, rightErr := url.Parse(second)
	if leftErr != nil || rightErr != nil || left.Scheme == "" || right.Scheme == "" {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}
