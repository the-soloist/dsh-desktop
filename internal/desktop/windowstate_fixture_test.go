package desktop

import (
	"fmt"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Keep both frames inside the logical work area, including space for native
// creation's titlebar, and make both dimensions change during the resize test.
func windowStateFixtureSizes(area application.Rect) (initial, resized application.Size, err error) {
	const margin, step = 64, 16
	resized = application.Size{Width: min(1320, area.Width-margin), Height: min(820, area.Height-margin)}
	if resized.Width < minimumWidth+step || resized.Height < minimumHeight+step {
		return initial, resized, fmt.Errorf("work area %+v cannot fit two test frames above minimum %dx%d with %d points of margin", area, minimumWidth, minimumHeight, margin)
	}
	initial = application.Size{Width: min(1100, resized.Width-step), Height: min(720, resized.Height-step)}
	return initial, resized, nil
}

func TestWindowStateFixtureSizes(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		wantError     bool
	}{
		{name: "desktop", width: 1920, height: 1080},
		{name: "small CI work area", width: 1280, height: 677},
		{name: "Retina logical work area", width: 1200, height: 700},
		{name: "minimum usable area", width: 880, height: 640},
		{name: "too narrow", width: 879, height: 640, wantError: true},
		{name: "too short", width: 880, height: 639, wantError: true},
		{name: "missing screen", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			area := application.Rect{X: 200, Y: 30, Width: test.width, Height: test.height}
			initial, resized, err := windowStateFixtureSizes(area)
			if (err != nil) != test.wantError {
				t.Fatalf("sizes = %+v -> %+v, error = %v", initial, resized, err)
			}
			if err != nil {
				return
			}
			for _, size := range []application.Size{initial, resized} {
				if size.Width < minimumWidth || size.Height < minimumHeight || size.Width >= area.Width || size.Height >= area.Height {
					t.Fatalf("frame %+v does not fit work area %+v above minimum", size, area)
				}
			}
			if initial.Width >= resized.Width || initial.Height >= resized.Height {
				t.Fatalf("resize must change both dimensions: %+v -> %+v", initial, resized)
			}
		})
	}
}
