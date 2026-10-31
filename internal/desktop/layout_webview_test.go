//go:build darwin && !server && layouttest

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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
	if fixture := os.Getenv("DSH_LAYOUT_CONTRACT_FIXTURE"); fixture != "" {
		if err := runLayoutContract(fixture); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// bun install --frozen-lockfile
// go test -tags layouttest ./internal/desktop -run TestLayoutWebView -v
func TestLayoutWebView(t *testing.T) {
	directory := t.TempDir()
	fixture := filepath.Join(directory, "fixture.js")
	build := exec.CommandContext(t.Context(), "bun", "build", "../../tests/fixtures/layout-contract.jsx", "--target=browser", "--format=iife", "--minify", "--define", `process.env.NODE_ENV="production"`, "--outfile", fixture)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build React fixture (run bun install): %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0])
	command.Env = append(os.Environ(), "DSH_LAYOUT_CONTRACT_FIXTURE="+fixture)
	command.WaitDelay = time.Second
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil || !strings.Contains(string(output), "PASS real React layout contract") {
		t.Fatalf("React layout contract: %v", err)
	}
}

type layoutContractSample struct{ Sidebar, Rightbar, Discoveries int }

func runLayoutContract(fixture string) error {
	source, err := os.ReadFile(fixture)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	origin := "http://" + listener.Addr().String()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fixture.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write(source)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><html><body><div id="root"></div><script src="/fixture.js"></script></body></html>`)
	})}
	go server.Serve(listener)
	defer server.Close()
	logger := log.New(io.Discard, "", 0)
	store := windowstate.New(filepath.Join(filepath.Dir(fixture), "state.json"), windowstate.Bounds{DefaultWidth: 1200, DefaultHeight: 780}, logger)
	if err := store.SaveWidth("sidebar", 350); err != nil {
		return err
	}
	if err := store.SaveWidth("rightbar", 520); err != nil {
		return err
	}
	c := &controller{layout: &layoutBridge{preferences: store, logger: logger}, authenticationProxy: &dshAuthenticationProxy{url: origin}, logger: logger}
	loaded := make(chan struct{}, 8)
	samples := make(chan layoutContractSample, 8)
	unsupported := make(chan struct{}, 8)
	app := application.New(application.Options{
		Name: "DSH Layout Contract", Assets: application.AssetOptions{Handler: startupAssetHandler("DSH Layout Contract")},
		RawMessageHandler: func(window application.Window, message string, originInfo *application.OriginInfo) {
			var envelope struct{ Type, Operation string }
			if json.Unmarshal([]byte(message), &envelope) != nil {
				return
			}
			switch envelope.Type {
			case "layout-contract-ready":
				loaded <- struct{}{}
			case "layout-contract-sample":
				var sample layoutContractSample
				if json.Unmarshal([]byte(message), &sample) == nil {
					samples <- sample
				}
			default:
				c.handleRawMessage(window, message, originInfo)
				if envelope.Operation == "unsupported" {
					unsupported <- struct{}{}
				}
			}
		},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", URL: "/", Width: 1400, Height: 800})
	manager := &windowManager{window: window, zoom: newPageZoom(window, logger)}
	manager.bindPageEnhancements()
	var navigate sync.Once
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) { navigate.Do(func() { window.SetURL(origin) }) })
	go func() {
		check := func() error {
			sample := func(script string) layoutContractSample {
				window.ExecJS(`(async function () { ` + script + `; window.webkit.messageHandlers.external.postMessage(JSON.stringify({type:"layout-contract-sample", ...window.layoutFixture.sample()})); })();`)
				return <-samples
			}
			waitWidths := func(sidebar, rightbar int) (layoutContractSample, error) {
				deadline := time.Now().Add(5 * time.Second)
				for {
					got := sample("")
					if got.Sidebar == sidebar && got.Rightbar == rightbar {
						return got, nil
					}
					if time.Now().After(deadline) {
						return got, fmt.Errorf("widths = %+v, want %d/%d", got, sidebar, rightbar)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			<-loaded
			baseline, err := waitWidths(350, 520)
			if err != nil {
				return err
			}
			if got := sample("await window.layoutFixture.stream()"); got.Discoveries != baseline.Discoveries {
				return fmt.Errorf("streaming triggered document rediscovery: %+v -> %+v", baseline, got)
			}
			sample(`window.layoutFixture.drag("sidebar", 375); window.layoutFixture.drag("rightbar", 600)`)
			if _, err := waitWidths(375, 600); err != nil {
				return err
			}
			deadline := time.Now().Add(5 * time.Second)
			for store.LoadLayout() != (windowstate.Layout{SidebarWidth: 375, RightbarWidth: 600}) {
				if time.Now().After(deadline) {
					return fmt.Errorf("drag widths were not saved: %+v", store.LoadLayout())
				}
				time.Sleep(20 * time.Millisecond)
			}
			sample("window.layoutFixture.render()")
			if _, err := waitWidths(375, 600); err != nil {
				return err
			}
			window.Reload()
			<-loaded
			if _, err := waitWidths(375, 600); err != nil {
				return err
			}
			sample("window.layoutFixture.render(false)")
			select {
			case <-unsupported:
			case <-time.After(5 * time.Second):
				return fmt.Errorf("incompatible DSH layout was not reported")
			}
			sample("window.layoutFixture.render(true)")
			_, err = waitWidths(375, 600)
			return err
		}
		if err := check(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("PASS real React layout contract: restore, drag, streaming, remount, reload, incompatibility reporting")
		app.Quit()
	}()
	return app.Run()
}
