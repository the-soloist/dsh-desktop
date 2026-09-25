package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Snapshot struct {
	Home     string
	Selected string
	Active   string
	Entries  []Entry
}

// Manager keeps requested and successfully started profiles separate. Failed
// switches never overwrite the last successful selection on disk.
type Manager struct {
	mu    sync.Mutex
	path  string
	state Snapshot
}

func Path(applicationName string) (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, applicationName, "profile.json"), nil
}

func New(path string) *Manager {
	return &Manager{path: path, state: Snapshot{Selected: Default, Entries: []Entry{{Name: Default}}}}
}

func (manager *Manager) Snapshot() Snapshot {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.state
	state.Entries = append([]Entry(nil), state.Entries...)
	return state
}

func (manager *Manager) Refresh(home string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if home == "" {
		return fmt.Errorf("DSH_HOME 尚未确定")
	}
	entries, err := Discover(home)
	if err != nil {
		return err
	}
	if manager.state.Home != home {
		manager.state = Snapshot{Home: home, Selected: Default}
		var saved selection
		data, _ := os.ReadFile(manager.path)
		if json.Unmarshal(data, &saved) == nil && saved.Home == home {
			for _, entry := range entries {
				if entry.Name == saved.Name && entry.Reason == "" {
					manager.state.Selected = saved.Name
				}
			}
		}
	}
	manager.state.Entries = entries
	found := false
	for _, entry := range entries {
		found = found || entry.Name == manager.state.Selected
	}
	if !found {
		manager.state.Entries = append(manager.state.Entries, Entry{Name: manager.state.Selected, Reason: "配置已移除"})
	}
	return nil
}

func (manager *Manager) Select(name string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.state.Home == "" {
		return fmt.Errorf("DSH_HOME 尚未确定")
	}
	if entry := Inspect(manager.state.Home, name); entry.Reason != "" {
		return fmt.Errorf("无法使用 Profile %q：%s", name, entry.Reason)
	}
	manager.state.Selected = name
	return nil
}

func (manager *Manager) ClearActive() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.state.Active = ""
}

func (manager *Manager) MarkReady() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.state.Active = manager.state.Selected
	if manager.path == "" {
		return nil
	}
	data, err := json.Marshal(selection{Home: manager.state.Home, Name: manager.state.Selected})
	if err != nil {
		return err
	}
	directory := filepath.Dir(manager.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), manager.path)
}

type selection struct {
	Home string `json:"home"`
	Name string `json:"name"`
}
