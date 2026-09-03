package desktop

import (
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
