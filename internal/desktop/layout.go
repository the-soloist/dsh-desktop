package desktop

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const layoutMessageType = "dsh-desktop-layout"

//go:embed layout.js
var layoutPersistenceScript string

//go:embed layout_adapter.js
var layoutAdapterScript string

type layoutPreferenceStore interface {
	LoadLayout() windowstate.Layout
	SaveWidth(side string, width int) error
}

type layoutBridge struct {
	preferences layoutPreferenceStore
	logger      *log.Logger
}

type desktopLayoutRequest struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Operation string `json:"operation"`
	Side      string `json:"side"`
	Width     int    `json:"width"`
}

func (controller *controller) handleLayoutMessage(window application.Window, message string, originInfo *application.OriginInfo) {
	var request desktopLayoutRequest
	if json.Unmarshal([]byte(message), &request) != nil || len(request.ID) == 0 || len(request.ID) > 128 {
		return
	}
	if !allowedPageMessageOrigin(window, request.URL, controller.activePageOrigin(), originInfo) {
		return
	}
	if script := controller.layout.handle(request); script != "" {
		window.ExecJS(script)
	}
}

func (bridge *layoutBridge) handle(request desktopLayoutRequest) string {
	if request.Operation == "load" {
		response, _ := json.Marshal(struct {
			ID     string             `json:"id"`
			Layout windowstate.Layout `json:"layout"`
		}{ID: request.ID, Layout: bridge.preferences.LoadLayout()})
		return fmt.Sprintf("window.__dshDesktopLayout && window.__dshDesktopLayout.restore(%s);", response)
	}
	if request.Operation == "unsupported" {
		bridge.logger.Printf("[layout] sidebar width persistence unavailable: incompatible DSH layout")
	}
	if request.Operation == "save" {
		if err := bridge.preferences.SaveWidth(request.Side, request.Width); err != nil && !errors.Is(err, windowstate.ErrInvalidColumnWidth) {
			bridge.logger.Printf("[layout] cannot save sidebar width: %v", err)
		}
	}
	return ""
}
