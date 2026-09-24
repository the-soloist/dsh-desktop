package desktop

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestPageZoomUsesTenPercentStepsAndBounds(t *testing.T) {
	for _, minimum := range []int{50, 100} {
		for current := minimum; current <= maximumPageZoom; current += 10 {
			for _, direction := range []int{-1, 0, 1} {
				want := max(minimum, min(maximumPageZoom, current+10*direction))
				if direction == 0 {
					want = 100
				}
				if got := nextPageZoom(current, direction, minimum); got != want {
					t.Fatalf("nextPageZoom(%d, %d, %d) = %d, want %d", current, direction, minimum, got, want)
				}
			}
		}
	}
}

func TestPageZoomShortcutsAndNavigationRestore(t *testing.T) {
	var applied []int
	var shown []bool
	zoom := &pageZoom{percent: 100, apply: func(percent int, show bool) bool {
		applied = append(applied, percent)
		shown = append(shown, show)
		return true
	}}
	bindings := zoom.keyBindings()
	for _, key := range []string{"CmdOrCtrl+=", "CmdOrCtrl+plus", "CmdOrCtrl+-"} {
		bindings[key](nil)
	}
	zoom.restore() // Navigation must keep 110%, not silently return to 100%.
	if _, exists := bindings["CmdOrCtrl+0"]; exists {
		t.Fatal("zoom reset shortcut must not be registered")
	}
	if want := []int{110, 120, 110, 110}; !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied = %v, want %v", applied, want)
	}
	if want := []bool{true, true, true, false}; !reflect.DeepEqual(shown, want) {
		t.Fatalf("indicator visibility = %v, want %v", shown, want)
	}
}

func TestPageZoomDoesNotAdvanceWhenNativeViewUnavailable(t *testing.T) {
	zoom := &pageZoom{percent: 100, apply: func(int, bool) bool { return false }}
	zoom.change(1)
	if zoom.percent != 100 {
		t.Fatalf("level advanced without applying zoom: %d", zoom.percent)
	}
}

func TestPageZoomControlAliasesOnMac(t *testing.T) {
	var applied []int
	zoom := &pageZoom{percent: 100, apply: func(percent int, _ bool) bool {
		applied = append(applied, percent)
		return true
	}}
	bindings := zoom.keyBindingsForOS("darwin")
	for _, key := range []string{"Ctrl+=", "Ctrl+plus", "Ctrl+-"} {
		if bindings[key] == nil {
			t.Fatalf("missing shortcut %s", key)
		}
		bindings[key](nil)
	}
	bindings["CmdOrCtrl+-"](nil) // Both modifiers must operate on the same layout level.
	if want := []int{110, 120, 110, 100}; !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied = %v, want %v", applied, want)
	}
	for _, goos := range []string{"windows", "linux"} {
		if _, exists := zoom.keyBindingsForOS(goos)["Ctrl+="]; exists {
			t.Fatalf("duplicate Control alias on %s", goos)
		}
	}
}

func TestPageZoomDoesNotBindShiftCombinations(t *testing.T) {
	zoom := &pageZoom{}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		for key := range zoom.keyBindingsForOS(goos) {
			if strings.Contains(key, "Shift") {
				t.Fatalf("unexpected Shift shortcut on %s: %s", goos, key)
			}
		}
	}
}

func TestPageZoomSerialisesConcurrentShortcuts(t *testing.T) {
	var applied []int
	zoom := &pageZoom{percent: 100, apply: func(percent int, _ bool) bool {
		applied = append(applied, percent)
		return true
	}}
	var group sync.WaitGroup
	for range 5 {
		group.Go(func() { zoom.change(1) })
	}
	group.Wait()
	if want := []int{110, 120, 130, 140, 150}; !reflect.DeepEqual(applied, want) {
		t.Fatalf("concurrent zoom = %v, want %v", applied, want)
	}
}
