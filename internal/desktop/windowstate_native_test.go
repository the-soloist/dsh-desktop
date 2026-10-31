//go:build darwin && !server && windowstatetest

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/the-soloist/dsh-desktop/internal/windowstate"
	"github.com/wailsapp/wails/v3/pkg/application"
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
	for _, phase := range []string{"screen", "resize", "reopen", "maximise", "reopen-maximised", "reopen"} {
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

type windowStateFixturePlan struct {
	Screen           *application.Screen
	Initial, Resized application.Size
}

// Probe in a separate app launch: Wails populates its screen cache after the
// native event loop starts. Subsequent launches seed state before window creation.
func runWindowStateScreenProbe(directory string) error {
	app := application.New(application.Options{Name: "Window State Screen Probe"})
	go func() {
		var plan windowStateFixturePlan
		err := waitWindowState("screen metadata", func() error {
			plan.Screen = app.Screen.GetPrimary()
			if plan.Screen == nil {
				return fmt.Errorf("primary screen is unavailable")
			}
			return nil
		})
		if err == nil {
			plan.Initial, plan.Resized, err = windowStateFixtureSizes(plan.Screen.WorkArea)
		}
		if err == nil {
			var data []byte
			data, err = json.Marshal(plan)
			if err == nil {
				fmt.Println("native window test plan:", string(data))
				err = os.WriteFile(filepath.Join(directory, "plan.json"), data, 0o600)
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("PASS native window state: screen")
		app.Quit()
	}()
	return app.Run()
}

func runWindowStateTest(directory, phase string) error {
	if phase == "screen" {
		return runWindowStateScreenProbe(directory)
	}
	data, err := os.ReadFile(filepath.Join(directory, "plan.json"))
	if err != nil {
		return err
	}
	var plan windowStateFixturePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return err
	}
	logger := log.New(io.Discard, "", 0)
	path := filepath.Join(directory, "state.json")
	bounds := windowstate.Bounds{DefaultWidth: defaultWidth, DefaultHeight: defaultHeight, MinimumWidth: minimumWidth, MinimumHeight: minimumHeight}
	store := windowstate.New(path, bounds, logger)
	if phase == "resize" {
		store.Update(func(state *windowstate.State) { state.Width = plan.Initial.Width; state.Height = plan.Initial.Height })
		if err := store.Save(); err != nil {
			return err
		}
	}
	expected := store.Snapshot()
	want := plan.Resized
	if phase == "resize" {
		want = plan.Initial
	}
	if expected.Width != want.Width || expected.Height != want.Height || expected.Maximised != (phase == "reopen-maximised") {
		return fmt.Errorf("%s: saved state = %+v, want normal bounds %+v, maximised=%t", phase, expected, want, phase == "reopen-maximised")
	}
	fmt.Printf("phase=%s saved=%dx%d maximised=%t workArea=%+v scale=%g\n", phase, expected.Width, expected.Height, expected.Maximised, plan.Screen.WorkArea, plan.Screen.ScaleFactor)
	app := application.New(application.Options{
		Name:   "Window State Test",
		Assets: application.AssetOptions{Handler: startupAssetHandler("Window State Test")},
	})
	manager := newWindowManager(app, store, "Window State Test", logger)
	app.OnShutdown(manager.save)
	go func() {
		check := func() error {
			if err := waitWindowState("geometry initialisation", func() error {
				if !manager.geometry.ready.Load() {
					return fmt.Errorf("window runtime is not ready")
				}
				return nil
			}); err != nil {
				return err
			}
			normalBounds := func() error {
				width, height := manager.window.Size()
				state := store.Snapshot()
				if manager.window.IsMaximised() || width != expected.Width || height != expected.Height || state.Maximised || state.Width != width || state.Height != height {
					return fmt.Errorf("restored native dimensions = %dx%d, want %dx%d, maximised=%t, state=%+v", width, height, expected.Width, expected.Height, manager.window.IsMaximised(), state)
				}
				return nil
			}
			maximised := func() error {
				state := store.Snapshot()
				if !manager.window.IsMaximised() || !state.Maximised || state.Width != expected.Width || state.Height != expected.Height {
					return fmt.Errorf("maximisation must preserve normal bounds %dx%d: native maximised=%t, state=%+v", expected.Width, expected.Height, manager.window.IsMaximised(), state)
				}
				return nil
			}
			if expected.Maximised {
				if err := waitWindowState("restored maximised state", maximised); err != nil {
					return err
				}
				manager.window.UnMaximise()
			}
			if err := waitWindowState("restored normal bounds", normalBounds); err != nil {
				return err
			}
			if phase == "resize" {
				manager.window.SetSize(plan.Resized.Width, plan.Resized.Height)
				if err := waitWindowState("persisted native resize", func() error {
					width, height := manager.window.Size()
					state := windowstate.New(path, bounds, logger).Snapshot()
					if width != plan.Resized.Width || height != plan.Resized.Height || state.Width != width || state.Height != height {
						return fmt.Errorf("resize target=%+v, native=%dx%d, saved=%+v", plan.Resized, width, height, state)
					}
					return nil
				}); err != nil {
					return err
				}
			}
			if phase == "maximise" {
				manager.window.Maximise()
				return waitWindowState("maximisation", maximised)
			}
			return nil
		}
		if err := check(); err != nil {
			screens, _ := json.Marshal(app.Screen.GetAll())
			fmt.Fprintf(os.Stderr, "%s: %v; state=%+v; screens=%s\n", phase, err, store.Snapshot(), screens)
			os.Exit(1)
		}
		fmt.Println("PASS native window state:", phase)
		// Same shutdown path as a system menu exit, without controller.quit().
		app.Quit()
	}()
	return app.Run()
}

// Require the exact target to remain stable while native animations and resize
// events settle; report the last mismatch instead of relying on a startup sleep.
func waitWindowState(label string, check func() error) error {
	deadline := time.Now().Add(5 * time.Second)
	var matchedSince time.Time
	for {
		err := check()
		if err == nil {
			if matchedSince.IsZero() {
				matchedSince = time.Now()
			}
			if time.Since(matchedSince) >= 200*time.Millisecond {
				return nil
			}
		} else {
			matchedSince = time.Time{}
		}
		if time.Now().After(deadline) {
			if err == nil {
				return fmt.Errorf("timed out waiting for %s to remain stable", label)
			}
			return fmt.Errorf("timed out waiting for %s: %w", label, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
