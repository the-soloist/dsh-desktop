package desktop

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormaliseWindowState(t *testing.T) {
	tests := []struct {
		name  string
		input windowState
		want  windowState
	}{
		{
			name:  "valid state is preserved",
			input: windowState{X: -1200, Y: 30, Width: 1440, Height: 900, Maximised: true, HasPosition: true},
			want:  windowState{X: -1200, Y: 30, Width: 1440, Height: 900, Maximised: true, HasPosition: true},
		},
		{
			name:  "invalid dimensions use defaults",
			input: windowState{Width: 10, Height: 20000},
			want:  windowState{Width: defaultWidth, Height: defaultHeight},
		},
		{
			name:  "implausible position is discarded",
			input: windowState{X: 100001, Y: 20, Width: 900, Height: 600, HasPosition: true},
			want:  windowState{Width: 900, Height: 600},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normaliseWindowState(test.input)
			if got != test.want {
				t.Fatalf("normaliseWindowState() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestSetEnvironmentReplacesExistingValue(t *testing.T) {
	got := setEnvironment([]string{"PATH=/bin", "DSH_HOME=old", "OTHER=value"}, "DSH_HOME", "new")
	want := []string{"PATH=/bin", "OTHER=value", "DSH_HOME=new"}
	if len(got) != len(want) {
		t.Fatalf("setEnvironment() length = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("setEnvironment()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestPrependExecutablePaths(t *testing.T) {
	first := filepath.Join("", "opt", "homebrew", "bin")
	existing := filepath.Join("", "usr", "bin")
	separator := string(os.PathListSeparator)
	environment := []string{"OTHER=value", "PATH=" + existing}

	got := prependExecutablePaths(environment, first, first)
	gotPath := environmentValue(got, "PATH")
	wantPath := strings.Join([]string{first, existing}, separator)
	if gotPath != wantPath {
		t.Fatalf("PATH = %q, want %q", gotPath, wantPath)
	}
	if len(got) != len(environment) {
		t.Fatalf("environment length = %d, want %d: %#v", len(got), len(environment), got)
	}
}

func TestWaitForDSHRequiresStableReadiness(t *testing.T) {
	process := &managedProcess{done: make(chan struct{})}
	checks := 0
	err := waitForDSHWithProbe(process, 100*time.Millisecond, time.Millisecond, 4*time.Millisecond, func() bool {
		checks++
		return checks != 2
	})
	if err != nil {
		t.Fatalf("waitForDSHWithProbe() error = %v", err)
	}
	if checks < 5 {
		t.Fatalf("readiness checks = %d, want at least 5", checks)
	}
}

func TestWaitForDSHReportsExitDuringStabilityWindow(t *testing.T) {
	process := &managedProcess{done: make(chan struct{})}
	process.waitErr = errors.New("backend failed")
	close(process.done)

	err := waitForDSHWithProbe(process, time.Second, time.Millisecond, 10*time.Millisecond, func() bool { return true })
	if err == nil || !strings.Contains(err.Error(), "backend failed") {
		t.Fatalf("waitForDSHWithProbe() error = %v, want backend failure", err)
	}
}

func TestWaitForDSHSupportsExternalProcess(t *testing.T) {
	err := waitForDSHWithProbe(nil, 100*time.Millisecond, time.Millisecond, 3*time.Millisecond, func() bool { return true })
	if err != nil {
		t.Fatalf("waitForDSHWithProbe() error = %v", err)
	}
}

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
	handler := startupAssetHandler()
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
	recorder := newStartupOutputRecorder(&destination, func(line string) { lines = append(lines, line) })
	_, _ = io.WriteString(recorder, "Resolving dep")
	_, _ = io.WriteString(recorder, "endencies\nResolved 2\nerror without newline")
	if got := destination.String(); got != "Resolving dependencies\nResolved 2\nerror without newline" {
		t.Fatalf("destination = %q", got)
	}
	if len(lines) != 2 || lines[0] != "Resolving dependencies" || lines[1] != "Resolved 2" {
		t.Fatalf("observed lines = %#v", lines)
	}
	if !strings.Contains(recorder.recentOutput(), "error without newline") {
		t.Fatalf("recent output = %q", recorder.recentOutput())
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

func TestManagedProcessState(t *testing.T) {
	first := &managedProcess{}
	second := &managedProcess{}
	state := newManagedProcessState(first)

	if state.clearIfCurrent(second) {
		t.Fatal("clearIfCurrent() cleared a different process")
	}
	if got := state.take(); got != first {
		t.Fatalf("take() = %p, want %p", got, first)
	}
	if got := state.take(); got != nil {
		t.Fatalf("second take() = %p, want nil", got)
	}

	state.set(second)
	if !state.clearIfCurrent(second) {
		t.Fatal("clearIfCurrent() did not clear the current process")
	}
}
