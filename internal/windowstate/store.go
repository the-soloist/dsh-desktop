// Package windowstate persists the desktop window's normal bounds.
package windowstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Logger is the minimal logging contract used by Store.
type Logger interface {
	Printf(format string, values ...any)
}

// State is the persisted normal window geometry.
type State struct {
	X           int  `json:"x"`
	Y           int  `json:"y"`
	Width       int  `json:"width"`
	Height      int  `json:"height"`
	Maximised   bool `json:"maximised"`
	HasPosition bool `json:"hasPosition"`
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
	state := store.Snapshot()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(store.path, append(data, '\n'), 0o600)
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
	return state
}
