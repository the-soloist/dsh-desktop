package desktop

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	clipboardMessageType = "dsh-desktop-clipboard-write"
	maximumClipboardText = 16 << 20
)

//go:embed clipboard.js
var clipboardInterceptorScript string

type desktopClipboardRequest struct {
	ID   string  `json:"id"`
	URL  string  `json:"url"`
	Text *string `json:"text"`
}

func (controller *controller) handleClipboardMessage(window application.Window, message string, originInfo *application.OriginInfo, setText func(string) bool) {
	var request desktopClipboardRequest
	if json.Unmarshal([]byte(message), &request) != nil || request.ID == "" || len(request.ID) > 128 {
		return
	}
	if !allowedPageMessageOrigin(window, request.URL, controller.activePageOrigin(), originInfo) {
		controller.logger.Printf("[clipboard] rejected request from outside the active DSH page")
		return
	}
	ok := writeClipboardText(request.Text, setText)
	if !ok {
		// Do not log the clipboard contents: messages may contain secrets.
		controller.logger.Printf("[clipboard] cannot write text to the native clipboard")
	}
	window.ExecJS(clipboardResponseScript(request.ID, ok))
}

func writeClipboardText(text *string, setText func(string) bool) bool {
	// Native clipboard APIs use NUL-terminated strings. Reject rather than
	// reporting success for silently truncated text.
	return text != nil && len(*text) <= maximumClipboardText && !strings.ContainsRune(*text, '\x00') && setText(*text)
}

func clipboardResponseScript(id string, ok bool) string {
	response, _ := json.Marshal(struct {
		ID string `json:"id"`
		OK bool   `json:"ok"`
	}{ID: id, OK: ok})
	return fmt.Sprintf("window.__dshDesktopClipboard && window.__dshDesktopClipboard.complete(%s);", response)
}
