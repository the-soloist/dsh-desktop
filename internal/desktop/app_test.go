package desktop

import (
	"bytes"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartupTimelinePreservesStatusHistoryAndStartTimes(t *testing.T) {
	timeline := newStartupTimeline("第一步", "准备")
	update := timeline.append("第二步", "启动", false, true)
	if len(update.Steps) != 1 || update.Steps[0].Status != "第二步" || !update.Steps[0].Navigate {
		t.Fatalf("append update = %#v", update)
	}
	snapshot := timeline.snapshot()
	if !snapshot.Reset || len(snapshot.Steps) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for _, step := range snapshot.Steps {
		if _, err := time.Parse(time.RFC3339Nano, step.StartedAt); err != nil {
			t.Fatalf("invalid startedAt %q: %v", step.StartedAt, err)
		}
	}
}

func TestStartupTimelineResetClearsPreviousStatuses(t *testing.T) {
	timeline := newStartupTimeline("旧状态一", "")
	timeline.append("旧状态二", "", false, false)
	timeline.reset("新状态", "", false)
	snapshot := timeline.snapshot()
	if len(snapshot.Steps) != 1 || snapshot.Steps[0].Status != "新状态" {
		t.Fatalf("snapshot after reset = %#v", snapshot)
	}
}

func TestStartupTimelineMarksCommandsForDedicatedRendering(t *testing.T) {
	timeline := newStartupTimeline("准备", "")
	update := timeline.appendCommand("正在启动 DSH", "bunx example@1.0.0 web --no-open")
	if len(update.Steps) != 1 || !update.Steps[0].Code {
		t.Fatalf("appendCommand() update = %#v", update)
	}
}

func TestStartupAssets(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/", contentType: "text/html", contains: `/logo.png`},
		{path: "/styles.css", contentType: "text/css", contains: ".step-heading time"},
		{path: "/app.js", contentType: "text/javascript", contains: `startup:frontend-ready`},
		{path: "/logo.png", contentType: "image/png"},
	}
	handler := startupAssetHandler("DSH Desktop")
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest("GET", test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("status = %d", response.Code)
			}
			if got := response.Header().Get("Content-Type"); !strings.Contains(got, test.contentType) {
				t.Fatalf("Content-Type = %q, want %q", got, test.contentType)
			}
			if test.contains != "" && !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("response does not contain %q", test.contains)
			}
		})
	}
}

func TestStartupOutputRecorder(t *testing.T) {
	var destination bytes.Buffer
	var lines []string
	recorder := newStartupOutputRecorder(log.New(&destination, "", 0), func(line string) { lines = append(lines, line) })
	_, _ = io.WriteString(recorder, "Resolving dep")
	_, _ = io.WriteString(recorder, "endencies\nResolved 2\nerror without newline")
	recorder.Flush()
	if got := destination.String(); got != "[dsh] Resolving dependencies\n[dsh] Resolved 2\n[dsh] error without newline\n" {
		t.Fatalf("destination = %q", got)
	}
	if len(lines) != 3 || lines[0] != "Resolving dependencies" || lines[1] != "Resolved 2" || lines[2] != "error without newline" {
		t.Fatalf("observed lines = %#v", lines)
	}
	if !strings.Contains(recorder.recentOutput(), "error without newline") {
		t.Fatalf("recent output = %q", recorder.recentOutput())
	}
}

func TestStartupOutputRedactsTokenAndSummarisesPeerWarnings(t *testing.T) {
	var destination bytes.Buffer
	recorder := newStartupOutputRecorder(log.New(&destination, "", 0), nil)
	_, _ = io.WriteString(recorder, "warn: incorrect peer dependency one\n")
	_, _ = io.WriteString(recorder, "warn: incorrect peer dependency two\n")
	_, _ = io.WriteString(recorder, "dsh web: http://127.0.0.1:3080/?token=secret-value\n")
	recorder.Flush()
	output := destination.String()
	if strings.Contains(output, "secret-value") || !strings.Contains(output, "token=<redacted>") {
		t.Fatalf("sanitized output = %q", output)
	}
	if !strings.Contains(output, "已省略 2 条 peer dependency 警告") {
		t.Fatalf("warning summary missing from %q", output)
	}
	if strings.Contains(recorder.recentOutput(), "secret-value") {
		t.Fatalf("recent output leaked token: %q", recorder.recentOutput())
	}
}

func TestDSHWebURLAcceptsOnlyConfiguredOrigin(t *testing.T) {
	const expected = "http://127.0.0.1:3080"
	got, ok := dshWebURL("dsh web: http://127.0.0.1:3080/?token=secret", expected)
	if !ok || got != "http://127.0.0.1:3080/?token=secret" || !hasDSHAuthenticationToken(got) {
		t.Fatalf("dshWebURL() = %q, %v", got, ok)
	}
	if _, ok = dshWebURL("dsh web: https://example.com/?token=secret", expected); ok {
		t.Fatal("dshWebURL() accepted an unexpected origin")
	}
}

func TestDSHOutputSummary(t *testing.T) {
	for _, line := range []string{"Resolving dependencies", "Resolved, downloaded and extracted [2]", "dsh web: http://127.0.0.1:3080"} {
		if _, _, _, ok := dshOutputSummary(line); !ok {
			t.Fatalf("dshOutputSummary(%q) was not recognised", line)
		}
	}
	if _, _, _, ok := dshOutputSummary("ordinary log message"); ok {
		t.Fatal("ordinary output was exposed as a startup summary")
	}
}

func TestPageReloadKeyBindings(t *testing.T) {
	bindings := pageReloadKeyBindings()
	for _, shortcut := range []string{"CmdOrCtrl+R", "F5"} {
		if bindings[shortcut] == nil {
			t.Fatalf("missing page reload binding %q", shortcut)
		}
	}
	if len(bindings) != 2 {
		t.Fatalf("page reload binding count = %d, want 2", len(bindings))
	}
}

func TestSmokeTestEnabledUsesEnvironment(t *testing.T) {
	t.Setenv("DSH_SMOKE_TEST", "true")
	if !smokeTestEnabled() {
		t.Fatal("smokeTestEnabled() = false, want true")
	}
}

func TestHeadlessSmokeTestEnabledUsesEnvironment(t *testing.T) {
	t.Setenv("DSH_HEADLESS_SMOKE_TEST", "1")
	if !headlessSmokeTestEnabled() {
		t.Fatal("headlessSmokeTestEnabled() = false, want true")
	}
}

func TestDSHLaunchCommandIncludesExactVersion(t *testing.T) {
	got := dshLaunchCommand("bunx", "@deepseek-ai/dsh@0.1.2-rc.1")
	want := "bunx @deepseek-ai/dsh@0.1.2-rc.1 web --no-open"
	if got != want {
		t.Fatalf("dshLaunchCommand() = %q, want %q", got, want)
	}
}
