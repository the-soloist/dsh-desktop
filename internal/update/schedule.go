package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Schedule persists the last completed update-check attempt so application
// restarts cannot bypass the configured interval.
type Schedule struct {
	path     string
	interval time.Duration
	mu       sync.Mutex
}

// NewSchedule creates a persisted update-check schedule.
func NewSchedule(path string, interval time.Duration) *Schedule {
	if interval <= 0 {
		interval = 72 * time.Hour
	}
	return &Schedule{path: path, interval: interval}
}

// Path returns the platform-native configuration path for update-check state.
func Path(applicationName string) (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, applicationName, "update-check.json"), nil
}

// Due reports whether a check should run at now. Missing or malformed state
// is treated as due so a damaged state file never disables update checks.
func (schedule *Schedule) Due(now time.Time) bool {
	schedule.mu.Lock()
	defer schedule.mu.Unlock()
	data, err := os.ReadFile(schedule.path)
	if err != nil {
		return true
	}
	var state persistedSchedule
	if err := json.Unmarshal(data, &state); err != nil || state.LastCheckedAt.IsZero() {
		return true
	}
	if now.Before(state.LastCheckedAt) {
		return true
	}
	return now.Sub(state.LastCheckedAt) >= schedule.interval
}

// Mark records a completed check attempt atomically with owner-only access.
func (schedule *Schedule) Mark(now time.Time) error {
	schedule.mu.Lock()
	defer schedule.mu.Unlock()
	data, err := json.Marshal(persistedSchedule{LastCheckedAt: now.UTC()})
	if err != nil {
		return err
	}
	directory := filepath.Dir(schedule.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".update-check-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, schedule.path); err != nil {
		if !os.IsExist(err) {
			return err
		}
		if err := os.Remove(schedule.path); err != nil {
			return err
		}
		return os.Rename(temporaryPath, schedule.path)
	}
	return nil
}

type persistedSchedule struct {
	LastCheckedAt time.Time `json:"lastCheckedAt"`
}
