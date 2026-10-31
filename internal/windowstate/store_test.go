package windowstate

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"
	"time"
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

func TestStoreSavesResizeWithoutAnExitCallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	logger := log.New(io.Discard, "", 0)
	store := New(path, testBounds, logger)
	synctest.Test(t, func(t *testing.T) {
		for width := 1300; width <= 1400; width += 10 {
			store.Update(func(state *State) { state.Width = width; state.Height = 900 })
			store.SaveSoon()
		}
		time.Sleep(201 * time.Millisecond)
		synctest.Wait()
		state := New(path, testBounds, logger).Snapshot()
		if state.Width != 1400 || state.Height != 900 {
			t.Fatalf("resize was not persisted: %+v", state)
		}
	})
}

func TestSaveFlushesPendingResizeAndPreservesColumnWidths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	logger := log.New(io.Discard, "", 0)
	store := New(path, testBounds, logger)
	store.Update(func(state *State) {
		state.Width = 1500
		state.Height = 980
		state.Layout = Layout{SidebarWidth: 345, RightbarWidth: 510}
	})
	store.SaveSoon()
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	state := New(path, testBounds, logger).Snapshot()
	if state != store.Snapshot() {
		t.Fatalf("restored state = %+v", state)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary state files remain: %v, %v", entries, err)
	}
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && (info.Mode().Perm()&0o077) != 0) {
		t.Fatalf("state permissions = %v, %v", info, err)
	}
}

func TestConcurrentSavesCannotOverwriteNewerState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	logger := log.New(io.Discard, "", 0)
	store := New(path, testBounds, logger)
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			store.Update(func(state *State) { state.Width++; state.Layout.SidebarWidth++ })
			if err := store.Save(); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if got := New(path, testBounds, logger).Snapshot(); got != store.Snapshot() {
		t.Fatalf("stale or corrupt state: %+v", got)
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
