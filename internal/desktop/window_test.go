package desktop

import (
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
)

func TestWindowIntersectsScreens(t *testing.T) {
	screens := []*application.Screen{{WorkArea: application.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}}}
	visible := windowstate.State{X: 1800, Y: 900, Width: 800, Height: 600, HasPosition: true}
	if !windowIntersectsScreens(visible, screens) {
		t.Fatal("partially visible window was rejected")
	}
	offscreen := windowstate.State{X: 2500, Y: 0, Width: 800, Height: 600, HasPosition: true}
	if windowIntersectsScreens(offscreen, screens) {
		t.Fatal("off-screen window was accepted")
	}
}

type geometryTestWindow struct {
	width, height                    int
	maximised, minimised, fullscreen bool
}

func (window geometryTestWindow) Size() (int, int)     { return window.width, window.height }
func (window geometryTestWindow) Position() (int, int) { return 40, 60 }
func (window geometryTestWindow) IsMaximised() bool    { return window.maximised }
func (window geometryTestWindow) IsMinimised() bool    { return window.minimised }
func (window geometryTestWindow) IsFullscreen() bool   { return window.fullscreen }

func TestCaptureNormalGeometryPreservesSavedBoundsUntilNativeWindowExists(t *testing.T) {
	store := windowstate.New(filepath.Join(t.TempDir(), "state.json"), windowstate.Bounds{
		DefaultWidth: defaultWidth, DefaultHeight: defaultHeight, MinimumWidth: minimumWidth, MinimumHeight: minimumHeight,
	}, log.New(io.Discard, "", 0))
	store.Update(func(state *windowstate.State) { state.Width = 1450; state.Height = 920 })
	for _, window := range []geometryTestWindow{
		{}, {width: 0, height: 920}, {width: 1450, height: 0},
		{width: 400, height: 300}, {width: 1450, height: 920, maximised: true},
		{width: 1450, height: 920, minimised: true}, {width: 1450, height: 920, fullscreen: true},
	} {
		if captureNormalGeometry(window, store) {
			t.Fatalf("captured unavailable/non-normal geometry: %+v", window)
		}
		if state := store.Snapshot(); state.Width != 1450 || state.Height != 920 {
			t.Fatalf("saved dimensions were overwritten: %+v", state)
		}
	}
	if !captureNormalGeometry(geometryTestWindow{width: 1300, height: 860}, store) {
		t.Fatal("valid geometry was not captured")
	}
	state := store.Snapshot()
	if state.Width != 1300 || state.Height != 860 || state.X != 40 || state.Y != 60 || !state.HasPosition {
		t.Fatalf("captured geometry = %+v", state)
	}
}

func TestUnavailableNativeWindowPreservesMaximisedState(t *testing.T) {
	store := windowstate.New(filepath.Join(t.TempDir(), "state.json"), windowstate.Bounds{
		DefaultWidth: defaultWidth, DefaultHeight: defaultHeight,
	}, log.New(io.Discard, "", 0))
	store.Update(func(state *windowstate.State) { state.Maximised = true })
	before := store.Snapshot()
	geometry := &windowGeometry{window: &application.WebviewWindow{}, store: store}
	geometry.ready.Store(true)
	if geometry.record() || store.Snapshot() != before {
		t.Fatalf("unavailable native bounds overwrote state: %+v", store.Snapshot())
	}
}
