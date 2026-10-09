//go:build server

package desktop

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestWindowsZoomNativeDispatch(t *testing.T) {
	if os.Getenv("DSH_TEST_WINDOWS_ZOOM_DISPATCH") != "1" {
		// Wails owns a process-wide application singleton. Isolate the real
		// key-binding manager so this test does not affect other GUI fixtures.
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsZoomNativeDispatch$", "-test.v")
		command.Env = append(os.Environ(), "DSH_TEST_WINDOWS_ZOOM_DISPATCH=1")
		command.WaitDelay = time.Second
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("native-key dispatch test: %v\n%s", err, output)
		}
		return
	}

	// No App.Run, WebView, network server, DSH process or user configuration.
	app := application.New(application.Options{Name: "Windows zoom dispatch test"})
	window := &application.WebviewWindow{}
	applied := make(chan int, 8)
	zoom := &pageZoom{percent: 100, apply: func(percent int, show bool) bool {
		if !show {
			t.Error("keyboard zoom should show its percentage")
		}
		applied <- percent
		return true
	}}
	if runtime.GOOS == "windows" {
		// The Windows CI runner must also verify the production registration.
		zoom.bind(app, window)
	} else {
		zoom.bindWindowsKeys(app.KeyBinding, window)
	}
	if got := len(app.KeyBinding.GetAll()); got != 4 {
		t.Fatalf("native binding count = %d, want 4", got)
	}
	for _, step := range []struct {
		key  string
		want int
	}{
		{"Ctrl+OEM_PLUS", 110}, {"Ctrl+OEM_PLUS", 120},
		{"Ctrl+OEM_MINUS", 110}, {"Ctrl+OEM_MINUS", 100},
		{"Ctrl+ADD", 110}, {"Ctrl+SUBTRACT", 100},
	} {
		// Exercise Wails' actual lookup, not a direct call to our callback.
		if !app.KeyBinding.Process(step.key, window) {
			t.Fatalf("Windows key was not handled: %s", step.key)
		}
		select {
		case got := <-applied:
			if got != step.want {
				t.Fatalf("%s applied %d%%, want %d%%", step.key, got, step.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not apply zoom", step.key)
		}
	}
	for _, key := range []string{"Ctrl+OEM_COMMA", "OEM_PLUS", "Alt+OEM_PLUS", "Ctrl+0", "Ctrl+Shift+OEM_PLUS"} {
		if app.KeyBinding.Process(key, window) {
			t.Fatalf("unrelated shortcut was intercepted: %s", key)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		changes := 0
		scoped := &pageZoom{percent: 100, apply: func(int, bool) bool { changes++; return true }}
		scoped.bindWindowsKeys(app.KeyBinding, window)
		app.KeyBinding.Process("Ctrl+OEM_PLUS", &application.WebviewWindow{})
		app.KeyBinding.Process("Ctrl+OEM_PLUS", nil)
		synctest.Wait()
		if changes != 0 {
			t.Fatal("another window changed the main window zoom")
		}
		app.KeyBinding.Process("Ctrl+OEM_PLUS", window)
		synctest.Wait()
		if changes != 1 {
			t.Fatalf("main window changes = %d, want 1", changes)
		}
	})
	zoom.bindWindowsKeys(app.KeyBinding, window)
	// An accepted dispatch must not hold the UI caller until SetZoom finishes.
	// Holding the same mutex gives a deterministic check without sleeps.
	zoom.mu.Lock()
	returned := make(chan bool, 1)
	go func() { returned <- app.KeyBinding.Process("Ctrl+OEM_PLUS", window) }()
	select {
	case handled := <-returned:
		zoom.mu.Unlock()
		if !handled {
			t.Fatal("native dispatch unexpectedly declined the shortcut")
		}
	case <-time.After(5 * time.Second):
		zoom.mu.Unlock()
		t.Fatal("native dispatch blocked while zoom was busy")
	}
	select {
	case got := <-applied:
		if got != 110 {
			t.Fatalf("queued zoom = %d%%, want 110%%", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accepted shortcut did not complete")
	}
}
