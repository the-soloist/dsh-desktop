// Package windowstate persists the desktop window's normal bounds.
package windowstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger is the minimal logging contract used by Store.
type Logger interface {
	Printf(format string, values ...any)
}

// State is the persisted normal window geometry.
type State struct {
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Maximised   bool   `json:"maximised"`
	HasPosition bool   `json:"hasPosition"`
	Layout      Layout `json:"layout,omitempty"`
}

// Layout contains user-selected column widths in CSS pixels. Zero means that
// the desktop has no saved preference for that column.
type Layout struct {
	SidebarWidth  int `json:"sidebarWidth,omitempty"`
	RightbarWidth int `json:"rightbarWidth,omitempty"`
}

// Bounds defines valid and fallback window dimensions.
type Bounds struct {
	DefaultWidth  int
	DefaultHeight int
	MinimumWidth  int
	MinimumHeight int
}

// Store safely loads, updates, and saves one window state file.
type Store struct {
	path   string
	bounds Bounds
	logger Logger
	mu     sync.Mutex
	state  State
	saveMu sync.Mutex
	timer  *time.Timer
}

// Path returns the platform-native configuration path for applicationName.
func Path(applicationName string) (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, applicationName, "window-state.json"), nil
}

// New loads a window state store, falling back to default dimensions.
func New(path string, bounds Bounds, logger Logger) *Store {
	store := &Store{
		path:   path,
		bounds: bounds,
		logger: logger,
		state:  State{Width: bounds.DefaultWidth, Height: bounds.DefaultHeight},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Printf("cannot read window state: %v", err)
		}
		return store
	}
	var saved State
	if err = json.Unmarshal(data, &saved); err != nil {
		logger.Printf("cannot parse window state: %v", err)
		return store
	}
	store.state = store.normalise(saved)
	return store
}

// Snapshot returns a consistent copy of the current state.
func (store *Store) Snapshot() State {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.state
}

// Update applies one state change and restores dimension invariants.
func (store *Store) Update(change func(*State)) {
	store.mu.Lock()
	defer store.mu.Unlock()
	change(&store.state)
	store.state = store.normalise(store.state)
}

// Save persists the current state with owner-only permissions.
func (store *Store) Save() error {
	store.saveMu.Lock()
	defer store.saveMu.Unlock()
	store.mu.Lock()
	if store.timer != nil {
		store.timer.Stop()
		store.timer = nil
	}
	state := store.state
	store.mu.Unlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".window-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), store.path)
}

// SaveSoon coalesces move/resize bursts while keeping completed changes on disk
// independently of the application's exit path. Save flushes the latest state.
func (store *Store) SaveSoon() {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.timer != nil {
		store.timer.Stop()
	}
	store.timer = time.AfterFunc(200*time.Millisecond, func() {
		if err := store.Save(); err != nil {
			store.logger.Printf("cannot save window state: %v", err)
		}
	})
}

func (store *Store) normalise(state State) State {
	if state.Width < store.bounds.MinimumWidth || state.Width > 10000 {
		state.Width = store.bounds.DefaultWidth
	}
	if state.Height < store.bounds.MinimumHeight || state.Height > 10000 {
		state.Height = store.bounds.DefaultHeight
	}
	if state.X < -100000 || state.X > 100000 || state.Y < -100000 || state.Y > 100000 {
		state.X = 0
		state.Y = 0
		state.HasPosition = false
	}
	if state.Layout.SidebarWidth < 0 || state.Layout.SidebarWidth > MaximumColumnWidth {
		state.Layout.SidebarWidth = 0
	}
	if state.Layout.RightbarWidth < 0 || state.Layout.RightbarWidth > MaximumColumnWidth {
		state.Layout.RightbarWidth = 0
	}
	return state
}
