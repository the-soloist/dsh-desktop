package windowstate

import (
	"io"
	"log"
	"path/filepath"
	"testing"
)

var testBounds = Bounds{DefaultWidth: 1200, DefaultHeight: 780, MinimumWidth: 800, MinimumHeight: 560}

func TestStoreNormalisesAndPersistsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	logger := log.New(io.Discard, "", 0)
	store := New(path, testBounds, logger)
	store.Update(func(state *State) {
		state.Width = 10
		state.Height = 900
		state.X = -1200
		state.Y = 30
		state.HasPosition = true
	})
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded := New(path, testBounds, logger).Snapshot()
	if reloaded.Width != testBounds.DefaultWidth || reloaded.Height != 900 || reloaded.X != -1200 || !reloaded.HasPosition {
		t.Fatalf("reloaded state = %#v", reloaded)
	}
}

func TestStoreDiscardsImplausiblePosition(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "missing.json"), testBounds, log.New(io.Discard, "", 0))
	store.Update(func(state *State) {
		state.X = 100001
		state.Y = 20
		state.HasPosition = true
	})
	if state := store.Snapshot(); state.HasPosition || state.X != 0 || state.Y != 0 {
		t.Fatalf("normalised position = %#v", state)
	}
}
