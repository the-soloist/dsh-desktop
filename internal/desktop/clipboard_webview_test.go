//go:build darwin && !server && clipboardtest

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Run the child on Wails' main OS thread. The fixture uses no DSH process or
// user settings, and a fake writer leaves the user's actual clipboard intact.
func init() {
	if os.Getenv("DSH_CLIPBOARD_TEST_CHILD") == "1" {
		if err := runClipboardWebViewTest(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// Run: go test -tags clipboardtest ./internal/desktop -run TestClipboardWebView -v
func TestClipboardWebView(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0])
	command.Env = append(os.Environ(), "DSH_CLIPBOARD_TEST_CHILD=1")
	command.WaitDelay = time.Second
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil || !strings.Contains(string(output), "PASS native clipboard bridge") {
		t.Fatalf("native clipboard bridge: %v", err)
	}
}

func runClipboardWebViewTest() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	origin := "http://" + listener.Addr().String()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html><html><body>DSH clipboard fixture<script>
// Reproduce DSH's failing browser path before installing the desktop adapter.
const clipboard = navigator.clipboard || {};
Object.defineProperty(clipboard, 'writeText', { configurable: true, value: function () {
  return Promise.reject(new Error('NotAllowedError'));
}});
if (!navigator.clipboard) Object.defineProperty(navigator, 'clipboard', { configurable: true, value: clipboard });
function reportInstalled() {
  if (!window.__dshDesktopClipboard) { setTimeout(reportInstalled, 10); return; }
  window.webkit.messageHandlers.external.postMessage(JSON.stringify({ type: 'clipboard-test-loaded' }));
}
reportInstalled();
</script></body></html>`)
	})}
	defer server.Close()
	go server.Serve(listener)

	loaded := make(chan struct{}, 8)
	samples := make(chan bool, 8)
	writes := make(chan string, 8)
	logger := log.New(io.Discard, "", 0)
	controller := &controller{logger: logger, authenticationProxy: &dshAuthenticationProxy{url: origin}}
	app := application.New(application.Options{
		Name:   "DSH clipboard test",
		Assets: application.AssetOptions{Handler: startupAssetHandler("DSH clipboard test")},
		RawMessageHandler: func(window application.Window, message string, originInfo *application.OriginInfo) {
			var sample struct {
				Type   string `json:"type"`
				Copied bool   `json:"copied"`
			}
			if json.Unmarshal([]byte(message), &sample) != nil {
				return
			}
			if sample.Type == "clipboard-test-loaded" {
				loaded <- struct{}{}
				return
			}
			if sample.Type == "clipboard-test-result" {
				samples <- sample.Copied
				return
			}
			if sample.Type == clipboardMessageType {
				controller.handleClipboardMessage(window, message, originInfo, func(text string) bool {
					writes <- text
					return text != "simulate native failure"
				})
			}
		},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", URL: "/", Width: 800, Height: 560,
	})
	manager := &windowManager{window: window, zoom: newPageZoom(window, logger)}
	manager.bindPageEnhancements()
	var navigateOnce sync.Once
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		navigateOnce.Do(func() { window.SetURL(origin) })
	})
	go func() {
		check := func(text string, want bool) error {
			encoded, _ := json.Marshal(text)
			window.ExecJS(fmt.Sprintf(`(async function () {
  let copied = false;
  try { await navigator.clipboard.writeText(%s); copied = true; } catch (_) {}
  window.webkit.messageHandlers.external.postMessage(JSON.stringify({ type: 'clipboard-test-result', copied }));
})();`, encoded))
			if copied := <-samples; copied != want {
				return fmt.Errorf("copy result = %v, want %v", copied, want)
			}
			if got := <-writes; got != text {
				return fmt.Errorf("native clipboard bridge changed the copied text")
			}
			return nil
		}
		<-loaded
		for _, step := range []struct {
			text string
			ok   bool
		}{{"原样复制\n```js\nconst a = '你好 👋';\n```\n", true}, {"simulate native failure", false}} {
			if err := check(step.text, step.ok); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		for _, navigate := range []func(){window.Reload, func() { window.SetURL(origin + "/next") }} {
			navigate()
			<-loaded
			if err := check("after navigation", true); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		fmt.Println("PASS native clipboard bridge: permission-denied API, exact text, acknowledgement, failure, reload, navigation")
		app.Quit()
	}()
	return app.Run()
}
