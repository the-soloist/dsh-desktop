//go:build darwin && !server && profiletest

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

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	dshdesktop "github.com/the-soloist/dsh-desktop"
	"github.com/the-soloist/dsh-desktop/internal/backend"
	"github.com/the-soloist/dsh-desktop/internal/profile"
	"github.com/the-soloist/dsh-desktop/internal/windowstate"
)

// Wails locks the main OS thread during its init. Run this opt-in child on
// that thread before testing starts; it has no real DSH process or user state.
func init() {
	if directory := os.Getenv("DSH_PROFILE_TRAY_TEST_DIR"); directory != "" {
		if err := runProfileTrayTest(directory); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// Run with: go test -tags profiletest ./internal/desktop -run TestNativeProfileTray -v
func TestNativeProfileTray(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0])
	command.Env = append(os.Environ(), "DSH_PROFILE_TRAY_TEST_DIR="+t.TempDir())
	command.WaitDelay = 2 * time.Second
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil || !strings.Contains(string(output), "PASS native profile tray") {
		t.Fatalf("native tray test: %v", err)
	}
}

func runProfileTrayTest(directory string) error {
	for _, name := range []string{"web", "work"} {
		location := filepath.Join(directory, "profiles", name)
		if err := os.MkdirAll(location, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(location, "package.json"), []byte(`{"dsh":{"profile":{"bundles":["@deepseek-ai/dsh-web-app"]}}}`), 0o600); err != nil {
			return err
		}
	}
	logger := log.New(io.Discard, "", 0)
	app := application.New(application.Options{Name: "Profile Tray Test", Assets: application.AssetOptions{Handler: startupAssetHandler("Profile Tray Test")}})
	store := windowstate.New(filepath.Join(directory, "window.json"), windowstate.Bounds{DefaultWidth: 800, DefaultHeight: 560, MinimumWidth: 800, MinimumHeight: 560}, logger)
	window := newWindowManager(app, store, "Profile Tray Test", logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &controller{
		app: app, window: window, logger: logger, metadata: dshdesktop.Metadata{DisplayName: "Profile Tray Test"},
		backend: backend.NewSupervisor(backend.Config{Logger: logger}), profiles: profile.New(filepath.Join(directory, "selection.json")),
		serviceContext: ctx, cancelService: cancel, startup: newStartupTimeline("准备", "测试"),
	}
	if err := c.profiles.Refresh(directory); err != nil {
		return err
	}
	if err := c.profiles.MarkReady(); err != nil {
		return err
	}
	c.service.set(serviceReady)
	c.bindTray()
	ready := make(chan struct{})
	var once sync.Once
	window.window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) { once.Do(func() { close(ready) }) })
	go func() {
		<-ready
		if err := checkProfileTray(c); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("PASS native profile tray: default, repeated rebuild, switching, failure recovery, saved selection")
		app.Quit()
	}()
	return app.Run()
}

func checkProfileTray(c *controller) error {
	c.requestProfile("web")
	if c.service.current() != serviceReady {
		return fmt.Errorf("clicking current profile triggered a restart")
	}
	for range 10 {
		c.refreshProfileMenu()
	}
	c.requestProfile("work")
	if c.service.current() != serviceRestartPending || c.profiles.Snapshot().Selected != "work" || c.takeStartupIntent() != startupIntentRestart {
		return fmt.Errorf("switch did not schedule the selected profile")
	}
	c.requestProfile("web")
	if c.profiles.Snapshot().Selected != "work" {
		return fmt.Errorf("repeated switch was accepted during startup")
	}
	// A failure before StopCurrent leaves web active. Returning to it must
	// still be possible from the failure page, not mistaken for a no-op.
	c.service.set(serviceFailed)
	c.requestProfile("web")
	if c.profiles.Snapshot().Selected != "web" || c.service.current() != serviceRestartPending {
		return fmt.Errorf("cannot return to old profile after failed switch")
	}
	c.markProfileReady()
	c.requestProfile("work")
	c.profiles.ClearActive()
	c.markProfileReady()
	state := c.profiles.Snapshot()
	if state.Active != "work" || c.service.current() != serviceReady {
		return fmt.Errorf("successful switch did not become active")
	}
	loaded := profile.New(filepath.Join(state.Home, "selection.json"))
	if err := loaded.Refresh(state.Home); err != nil {
		return err
	}
	if loaded.Snapshot().Selected != "work" {
		return fmt.Errorf("successful profile was not restored")
	}
	return nil
}
