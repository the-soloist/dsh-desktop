package desktop

import (
	"errors"
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
