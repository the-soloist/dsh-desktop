package desktop

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type clipboardTestWindow struct {
	application.Window
	name    string
	scripts []string
}

func (window *clipboardTestWindow) Name() string { return window.name }
func (window *clipboardTestWindow) ExecJS(script string) {
	window.scripts = append(window.scripts, script)
}

func TestAllowedPageMessageOrigin(t *testing.T) {
	const origin = "http://127.0.0.1:43210"
	main := &clipboardTestWindow{name: "main"}
	for _, test := range []struct {
		name   string
		window application.Window
		url    string
		info   *application.OriginInfo
		want   bool
	}{
		{"macOS", main, origin + "/session", &application.OriginInfo{Origin: origin, IsMainFrame: true}, true},
		{"Windows", main, origin, &application.OriginInfo{Origin: origin + "/session", TopOrigin: origin + "/session"}, true},
		{"Linux", main, origin, &application.OriginInfo{Origin: origin + "/session"}, true},
		{"no window", nil, origin, &application.OriginInfo{Origin: origin}, false},
		{"other window", &clipboardTestWindow{name: "other"}, origin, &application.OriginInfo{Origin: origin}, false},
		{"missing origin", main, origin, nil, false},
		{"empty origin", main, origin, &application.OriginInfo{}, false},
		{"forged URL", main, origin, &application.OriginInfo{Origin: "https://example.test"}, false},
		{"external URL", main, "https://example.test", &application.OriginInfo{Origin: origin}, false},
		{"other local port", main, origin, &application.OriginInfo{Origin: "http://127.0.0.1:43211"}, false},
		{"untrusted top frame", main, origin, &application.OriginInfo{Origin: origin, TopOrigin: "https://example.test"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := allowedPageMessageOrigin(test.window, test.url, origin, test.info); got != test.want {
				t.Fatalf("allowedPageMessageOrigin() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestClipboardMessageReportsNativeResultWithoutLoggingText(t *testing.T) {
	const origin = "http://127.0.0.1:43210"
	const secret = "消息正文\nsecret-token 👋\n"
	for _, nativeSuccess := range []bool{true, false} {
		var logs bytes.Buffer
		controller := &controller{logger: log.New(&logs, "", 0), authenticationProxy: &dshAuthenticationProxy{url: origin}}
		window := &clipboardTestWindow{name: "main"}
		message, err := json.Marshal(map[string]any{"id": "page:1", "url": origin + "/session", "text": secret})
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		controller.handleClipboardMessage(window, string(message), &application.OriginInfo{Origin: origin}, func(text string) bool {
			calls++
			if text != secret {
				t.Fatalf("clipboard text = %q, want exact original", text)
			}
			return nativeSuccess
		})
		if calls != 1 || len(window.scripts) != 1 || window.scripts[0] != clipboardResponseScript("page:1", nativeSuccess) {
			t.Fatalf("calls=%d, responses=%v", calls, window.scripts)
		}
		if strings.Contains(logs.String(), "secret-token") || strings.Contains(window.scripts[0], "secret-token") {
			t.Fatal("clipboard text leaked into a log or acknowledgement")
		}
	}
}

func TestClipboardMessageRejectsUntrustedPage(t *testing.T) {
	controller := &controller{logger: log.New(&bytes.Buffer{}, "", 0), authenticationProxy: &dshAuthenticationProxy{url: "http://127.0.0.1:43210"}}
	window := &clipboardTestWindow{name: "main"}
	controller.handleClipboardMessage(window, `{"id":"page:1","url":"http://127.0.0.1:43210","text":"forged"}`,
		&application.OriginInfo{Origin: "https://example.test"}, func(string) bool {
			t.Fatal("untrusted page wrote to the clipboard")
			return true
		})
	if len(window.scripts) != 0 {
		t.Fatal("untrusted page received an acknowledgement")
	}
}

func TestWriteClipboardTextValidatesPayload(t *testing.T) {
	for _, text := range []string{"", "你好\nworld 👋\n", strings.Repeat("x", maximumClipboardText)} {
		calls := 0
		if !writeClipboardText(&text, func(got string) bool { calls++; return got == text }) || calls != 1 {
			t.Fatal("valid text did not reach the native clipboard exactly once")
		}
	}
	for _, text := range []string{"before\x00after", strings.Repeat("x", maximumClipboardText+1)} {
		if writeClipboardText(&text, func(string) bool { t.Fatal("invalid text reached native clipboard"); return true }) {
			t.Fatal("invalid text reported success")
		}
	}
	if writeClipboardText(nil, func(string) bool { t.Fatal("missing text reached native clipboard"); return true }) {
		t.Fatal("missing text reported success")
	}
}

func TestClipboardResponseEncodesRequestIDAsData(t *testing.T) {
	id := `</script>"; alert('not code'); //`
	script := clipboardResponseScript(id, true)
	encoded := strings.TrimSuffix(strings.TrimPrefix(script, "window.__dshDesktopClipboard && window.__dshDesktopClipboard.complete("), ");")
	var response struct {
		ID string `json:"id"`
		OK bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(encoded), &response); err != nil || response.ID != id || !response.OK {
		t.Fatalf("unsafe/malformed clipboard response: %s, %v", script, err)
	}
}
