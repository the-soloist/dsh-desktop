//go:build darwin && !server && zoomtest

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Opt-in native integration test. The child runs App.Run on the main thread
// without starting DSH or reading/writing the user's application settings.
// Run: go test -tags zoomtest ./internal/desktop -run TestPageZoomWebView -v
func TestMain(m *testing.M) {
	if os.Getenv("DSH_ZOOM_TEST_CHILD") == "1" {
		if err := runZoomWebViewTest(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestPageZoomWebView(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0])
	command.WaitDelay = 2 * time.Second
	command.Env = append(os.Environ(), "DSH_ZOOM_TEST_CHILD=1")
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil || !strings.Contains(string(output), "PASS native zoom, shortcuts, bounds, navigation, auto-hide") {
		t.Fatalf("native WebView zoom: %v", err)
	}
}

type zoomMetrics struct {
	Width, Height, BodyWidth, AppHeight, FooterBottom, Scale float64
	BadgeRight, BadgeBottom, BadgeWidth, Opacity             float64
	Label                                                    string
	Visible, Narrow                                          bool
}

const zoomFixture = `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
<style>
html, body { margin: 0; height: 100%; overflow: hidden; }
#app { height: 100vh; display: grid; grid-template-rows: 48px minmax(0,1fr) 64px; }
main { overflow: auto; } article { height: 2000px; }
footer { background: #293150; color: white; }
</style><script type="module" src="/wails/runtime.js"></script></head><body><div id="app"><header>Zoom layout fixture</header>
<main><article>Scrollable content</article><p>End of content</p></main><footer>Composer / bottom edge</footer></div></body></html>`

func runZoomWebViewTest() error {
	samples := make(chan zoomMetrics, 20)
	loaded := make(chan struct{}, 10)
	app := application.New(application.Options{
		Name: "DSH zoom test",
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" || r.URL.Path == "/next" {
				w.Header().Set("Content-Type", "text/html")
				io.WriteString(w, zoomFixture)
				return
			}
			http.NotFound(w, r)
		})},
		RawMessageHandler: func(_ application.Window, message string, _ *application.OriginInfo) {
			var sample zoomMetrics
			if json.Unmarshal([]byte(message), &sample) == nil && sample.Width > 0 {
				samples <- sample
			}
		},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", URL: "/", Width: 800, Height: 560})
	zoom := newPageZoom(window, log.New(os.Stderr, "", 0))
	zoom.bind(app, window)
	manager := &windowManager{window: window, zoom: zoom}
	manager.bindPageEnhancements()
	window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) { loaded <- struct{}{} })
	result := make(chan error, 1)
	go func() {
		err := checkZoomWebView(window, app, samples, loaded)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1) // NSApplication termination can exit without returning from App.Run.
		}
		result <- nil
		app.Quit()
	}()
	if err := app.Run(); err != nil {
		return err
	}
	return <-result
}

func checkZoomWebView(window *application.WebviewWindow, app *application.App, samples <-chan zoomMetrics, loaded <-chan struct{}) error {
	if shortcut := app.Menu.GetApplicationMenu().FindByRole(application.ResetZoom).GetAccelerator(); shortcut != "" {
		return fmt.Errorf("reset shortcut still installed: %s", shortcut)
	}
	<-loaded
	sample := func() zoomMetrics {
		// Allow native layout and the indicator's CSS transition to settle.
		time.Sleep(240 * time.Millisecond)
		window.ExecJS(`(() => {
          const badge = document.getElementById('dsh-desktop-zoom');
          const label = badge.shadowRoot.querySelector('span');
          const rect = label.getBoundingClientRect();
          window.webkit.messageHandlers.external.postMessage(JSON.stringify({
            width: innerWidth, height: innerHeight, bodyWidth: document.body.getBoundingClientRect().width,
            appHeight: document.getElementById('app').getBoundingClientRect().height,
            footerBottom: document.querySelector('footer').getBoundingClientRect().bottom,
            scale: visualViewport.scale, narrow: matchMedia('(max-width: 600px)').matches,
            badgeRight: innerWidth - rect.right, badgeBottom: innerHeight - rect.bottom,
            badgeWidth: rect.width, opacity: Number(getComputedStyle(label).opacity),
            label: label.textContent, visible: badge.hasAttribute('data-visible')
          }));
        })()`)
		return <-samples
	}
	baseline := sample()
	check := func(percent int, got zoomMetrics) error {
		factor := float64(percent) / 100
		if math.Abs(got.Width-baseline.Width/factor) > 2 || math.Abs(got.Height-baseline.Height/factor) > 2 ||
			math.Abs(got.BodyWidth-got.Width) > 2 || math.Abs(got.AppHeight-got.Height) > 2 ||
			math.Abs(got.FooterBottom-got.Height) > 2 || math.Abs(got.Scale-1) > .01 {
			return fmt.Errorf("cropped/non-reflowing layout at %d%%: %+v (base %+v)", percent, got, baseline)
		}
		if got.Label != fmt.Sprintf("%d%%", percent) || (got.Visible && (math.Abs(got.BadgeRight*factor-12) > 2 || math.Abs(got.BadgeBottom*factor-12) > 2)) {
			return fmt.Errorf("incorrect indicator at %d%%: %+v", percent, got)
		}
		fmt.Printf("PASS %d%%: viewport %.0fx%.0f, footer %.0f, badge %s\n", percent, got.Width, got.Height, got.FooterBottom, got.Label)
		return nil
	}
	for _, percent := range []int{110, 120, 130, 140, 150, 160, 170, 180, 190, 200} {
		window.HandleKeyEvent("Cmd+=")
		got := sample()
		if err := check(percent, got); err != nil {
			return err
		}
		if !got.Visible || got.Opacity != 1 {
			return fmt.Errorf("indicator is not visible after shortcut: %+v", got)
		}
		if percent == 200 && !got.Narrow {
			return fmt.Errorf("page zoom did not trigger responsive layout")
		}
	}
	window.HandleKeyEvent("Cmd+0")
	if err := check(200, sample()); err != nil {
		return fmt.Errorf("Cmd+0 unexpectedly changed zoom: %w", err)
	}
	for percent := 190; percent >= 50; percent -= 10 {
		window.HandleKeyEvent("Cmd+-")
		if err := check(percent, sample()); err != nil {
			return err
		}
	}
	window.HandleKeyEvent("Cmd+-")
	if err := check(50, sample()); err != nil {
		return err
	}
	time.Sleep(2100 * time.Millisecond)
	if got := sample(); got.Visible || got.Opacity != 0 {
		return fmt.Errorf("indicator did not auto-hide: %+v", got)
	}
	for _, navigate := range []func(){func() { window.SetURL("/next") }, window.Reload} {
		navigate()
		<-loaded
		got := sample()
		if err := check(50, got); err != nil {
			return err
		}
		if got.Visible {
			return fmt.Errorf("navigation should restore zoom without showing the indicator")
		}
	}
	fmt.Println(strings.Repeat("-", 32), "PASS native zoom, shortcuts, bounds, navigation, auto-hide")
	return nil
}
