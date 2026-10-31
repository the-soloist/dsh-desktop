package windowstate

import (
	"errors"
	"io"
	"log"
	"path/filepath"
	"testing"
)

func TestLayoutPreferencesPreserveGeometry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	logger := log.New(io.Discard, "", 0)
	store := New(path, testBounds, logger)
	store.Update(func(state *State) { state.Width = 1450; state.Height = 920; state.Maximised = true })
	for side, width := range map[string]int{"sidebar": 350, "rightbar": 520} {
		if err := store.SaveWidth(side, width); err != nil {
			t.Fatal(err)
		}
	}
	state := New(path, testBounds, logger).Snapshot()
	if state.Width != 1450 || state.Height != 920 || !state.Maximised || state.Layout != (Layout{SidebarWidth: 350, RightbarWidth: 520}) {
		t.Fatalf("layout update changed geometry: %+v", state)
	}
	for _, change := range []struct {
		side  string
		width int
	}{{"sidebar", 0}, {"rightbar", -1}, {"sidebar", MaximumColumnWidth + 1}, {"other", 350}} {
		if err := store.SaveWidth(change.side, change.width); !errors.Is(err, ErrInvalidColumnWidth) {
			t.Fatalf("invalid width error = %v", err)
		}
	}
	if store.Snapshot() != state {
		t.Fatal("invalid preference changed stored state")
	}
}
