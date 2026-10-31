//go:build darwin && !server && windowstatetest

package desktop

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func init() {
	if directory := os.Getenv("DSH_WINDOW_STATE_TEST_DIR"); directory != "" {
		if err := runWindowStateTest(directory, os.Getenv("DSH_WINDOW_STATE_TEST_PHASE")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// Run: go test -tags windowstatetest ./internal/desktop -run TestNativeWindowState -v
// Launches use temporary state files, without starting DSH or its tray.
func TestNativeWindowState(t *testing.T) {
	directory := t.TempDir()
	for _, phase := range []string{"resize", "reopen", "maximise", "reopen-maximised", "reopen"} {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		command := exec.CommandContext(ctx, os.Args[0])
		command.Env = append(os.Environ(), "DSH_WINDOW_STATE_TEST_DIR="+directory, "DSH_WINDOW_STATE_TEST_PHASE="+phase)
		command.WaitDelay = time.Second
		output, err := command.CombinedOutput()
		cancel()
		t.Log(string(output))
		if err != nil || !strings.Contains(string(output), "PASS native window state") {
			t.Fatalf("%s: %v", phase, err)
		}
	}
}

func runWindowStateTest(directory, phase string) error {
	logger := log.New(io.Discard, "", 0)
	path := filepath.Join(directory, "state.json")
	bounds := windowstate.Bounds{DefaultWidth: defaultWidth, DefaultHeight: defaultHeight, MinimumWidth: minimumWidth, MinimumHeight: minimumHeight}
	store := windowstate.New(path, bounds, logger)
	if phase == "resize" {
		store.Update(func(state *windowstate.State) { state.Width = 1100; state.Height = 720 })
		if err := store.Save(); err != nil {
			return err
		}
	}
	expected := store.Snapshot()
	app := application.New(application.Options{
		Name:   "Window State Test",
		Assets: application.AssetOptions{Handler: startupAssetHandler("Window State Test")},
	})
	manager := newWindowManager(app, store, "Window State Test", logger)
	app.OnShutdown(manager.save)
	ready := make(chan struct{})
	var once sync.Once
	manager.window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		once.Do(func() { close(ready) })
	})
	go func() {
		<-ready
		time.Sleep(600 * time.Millisecond)
		if expected.Maximised {
			if !manager.window.IsMaximised() {
				fmt.Fprintln(os.Stderr, "saved maximised state was not restored")
				os.Exit(1)
			}
			if state := store.Snapshot(); state.Width != expected.Width || state.Height != expected.Height {
				fmt.Fprintf(os.Stderr, "maximisation overwrote normal bounds: %+v\n", state)
				os.Exit(1)
			}
			manager.window.UnMaximise()
			time.Sleep(700 * time.Millisecond)
		}
		width, height := manager.window.Size()
		if width != expected.Width || height != expected.Height {
			fmt.Fprintf(os.Stderr, "restored native dimensions = %dx%d, saved = %dx%d\n", width, height, expected.Width, expected.Height)
			os.Exit(1)
		}
		if phase == "resize" {
			manager.window.SetSize(1320, 820)
			deadline := time.Now().Add(5 * time.Second)
			for {
				state := windowstate.New(path, bounds, logger).Snapshot()
				if state.Width == 1320 && state.Height == 820 {
					break
				}
				if time.Now().After(deadline) {
					fmt.Fprintf(os.Stderr, "native resize not saved before exit: %+v\n", state)
					os.Exit(1)
				}
				time.Sleep(30 * time.Millisecond)
			}
		}
		if phase == "maximise" {
			manager.window.Maximise()
			time.Sleep(600 * time.Millisecond)
			if !manager.window.IsMaximised() {
				fmt.Fprintln(os.Stderr, "could not maximise fixture")
				os.Exit(1)
			}
		}
		fmt.Println("PASS native window state:", phase)
		// Same shutdown path as a system menu exit, without controller.quit().
		app.Quit()
	}()
	return app.Run()
}
