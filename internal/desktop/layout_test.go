package desktop

import (
	"bytes"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestLayoutMessagePersistsEachColumnAcrossOrigins(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	path := filepath.Join(t.TempDir(), "state.json")
	bounds := windowstate.Bounds{DefaultWidth: 1200, DefaultHeight: 780, MinimumWidth: 800, MinimumHeight: 560}
	store := windowstate.New(path, bounds, logger)
	c := &controller{logger: logger,
		layout:              &layoutBridge{preferences: store, logger: logger},
		authenticationProxy: &dshAuthenticationProxy{url: "http://127.0.0.1:43210"},
	}
	window := &clipboardTestWindow{name: "main"}
	for _, message := range []string{
		`{"type":"dsh-desktop-layout","id":"page-1","url":"http://127.0.0.1:43210/session","operation":"save","side":"sidebar","width":350}`,
		`{"type":"dsh-desktop-layout","id":"page-1","url":"http://127.0.0.1:43210/session","operation":"save","side":"rightbar","width":520}`,
	} {
		c.handleRawMessage(window, message, &application.OriginInfo{Origin: c.authenticationProxy.URL()})
	}
	c.layout.preferences = windowstate.New(path, bounds, logger)
	c.authenticationProxy.url = "http://127.0.0.1:43211"
	c.handleRawMessage(window, `{"type":"dsh-desktop-layout","id":"page-2","url":"http://127.0.0.1:43211","operation":"load"}`,
		&application.OriginInfo{Origin: c.authenticationProxy.URL()})
	if len(window.scripts) != 1 || !strings.Contains(window.scripts[0], `"sidebarWidth":350`) || !strings.Contains(window.scripts[0], `"rightbarWidth":520`) || !strings.Contains(window.scripts[0], `"id":"page-2"`) {
		t.Fatalf("layout not restored at new proxy origin: %v", window.scripts)
	}
}

func TestLayoutMessageRejectsUntrustedOrInvalidChanges(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	store := windowstate.New(filepath.Join(t.TempDir(), "state.json"), windowstate.Bounds{DefaultWidth: 1200, DefaultHeight: 780}, logger)
	c := &controller{logger: logger,
		layout:              &layoutBridge{preferences: store, logger: logger},
		authenticationProxy: &dshAuthenticationProxy{url: "http://127.0.0.1:43210"},
	}
	window := &clipboardTestWindow{name: "main"}
	for _, message := range []string{
		`{"id":"x","url":"https://example.test","operation":"save","side":"sidebar","width":350}`,
		`{"id":"x","url":"http://127.0.0.1:43210","operation":"save","side":"sidebar","width":0}`,
		`{"id":"x","url":"http://127.0.0.1:43210","operation":"save","side":"sidebar","width":-1}`,
		`{"id":"x","url":"http://127.0.0.1:43210","operation":"save","side":"rightbar","width":10001}`,
		`{"id":"x","url":"http://127.0.0.1:43210","operation":"save","side":"other","width":350}`,
		`{"id":"x","url":"http://127.0.0.1:43210","operation":"save","side":"sidebar","width":350.5}`,
		`{"url":"http://127.0.0.1:43210","operation":"save","side":"sidebar","width":350}`,
	} {
		c.handleLayoutMessage(window, message, &application.OriginInfo{Origin: c.authenticationProxy.URL()})
	}
	message := `{"id":"x","url":"http://127.0.0.1:43210","operation":"save","side":"sidebar","width":350}`
	c.handleLayoutMessage(window, message, &application.OriginInfo{Origin: "https://example.test"})
	if store.LoadLayout() != (windowstate.Layout{}) || len(window.scripts) != 0 {
		t.Fatalf("invalid request changed state: %+v", store.LoadLayout())
	}
}

type memoryLayoutPreferences struct{ layout windowstate.Layout }

func (preferences *memoryLayoutPreferences) LoadLayout() windowstate.Layout {
	return preferences.layout
}
func (preferences *memoryLayoutPreferences) SaveWidth(side string, width int) error {
	if side == "sidebar" {
		preferences.layout.SidebarWidth = width
	} else {
		preferences.layout.RightbarWidth = width
	}
	return nil
}

func TestLayoutBridgeDoesNotRequireWindowManager(t *testing.T) {
	preferences := &memoryLayoutPreferences{}
	var output bytes.Buffer
	bridge := &layoutBridge{preferences: preferences, logger: log.New(&output, "", 0)}
	bridge.handle(desktopLayoutRequest{Operation: "save", Side: "sidebar", Width: 360})
	if script := bridge.handle(desktopLayoutRequest{Operation: "load", ID: "page"}); !strings.Contains(script, `"sidebarWidth":360`) {
		t.Fatalf("load result = %s", script)
	}
	bridge.handle(desktopLayoutRequest{Operation: "unsupported"})
	if !strings.Contains(output.String(), "incompatible DSH layout") {
		t.Fatal("incompatible layout was not reported")
	}
}
