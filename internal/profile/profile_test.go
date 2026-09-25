package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeProfile(t *testing.T, home, name string, bundles ...string) {
	t.Helper()
	directory := filepath.Join(home, "profiles", name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"dsh": map[string]any{"profile": map[string]any{"bundles": bundles}}})
	if err := os.WriteFile(filepath.Join(directory, "package.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverDefaultsToWebWithoutWriting(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-created")
	entries, err := Discover(home)
	if err != nil || !reflect.DeepEqual(entries, []Entry{{Name: "web"}}) {
		t.Fatalf("Discover = %v, %v", entries, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("profile discovery must not initialise DSH or its profiles")
	}
}

func TestDiscoverValidatesAndSortsProfiles(t *testing.T) {
	home := t.TempDir()
	writeProfile(t, home, "work", "@deepseek-ai/dsh-base", "@deepseek-ai/dsh-web-app")
	writeProfile(t, home, "custom", "my-web-bundle")
	writeProfile(t, home, "headless", "@deepseek-ai/dsh-base", "@deepseek-ai/dsh-headless")
	writeProfile(t, home, "broken")
	writeProfile(t, home, "node_modules", "ignore")
	writeProfile(t, home, "desktop", "ignore")
	writeProfile(t, home, ".hidden", "ignore")
	entries, err := Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name)
		if disabled := entry.Name == "broken" || entry.Name == "headless"; (entry.Reason != "") != disabled {
			t.Fatalf("unexpected availability: %+v", entry)
		}
	}
	if want := []string{"web", "broken", "custom", "headless", "work"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("profiles = %v", names)
	}
}

func TestValidateName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../work", `work\other`, "node_modules", "desktop", "DESKTOP", " work", "work\n", "--profile", `%PATH%`, `!PATH!`, `work"test`} {
		if ValidateName(name) == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, name := range []string{"web", "工作", "work profile", "work-test", "plugin"} {
		if err := ValidateName(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefreshKeepsRemovedSelectedProfileVisibleButDisabled(t *testing.T) {
	home := t.TempDir()
	writeProfile(t, home, "work", "@deepseek-ai/dsh-web-app")
	manager := New("")
	if err := manager.Refresh(home); err != nil {
		t.Fatal(err)
	}
	if err := manager.Select("work"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(home, "profiles", "work"), filepath.Join(home, "profiles", "renamed")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Refresh(home); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manager.Snapshot().Entries {
		if entry.Name == "work" && entry.Reason != "" {
			return
		}
	}
	t.Fatal("removed selection disappeared from the menu")
}

func TestSelectionPersistsOnlySuccessfulLaunches(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "profile.json")
	writeProfile(t, home, "work", "@deepseek-ai/dsh-web-app")
	manager := New(path)
	if manager.Snapshot().Selected != Default {
		t.Fatal("initial selection must be web")
	}
	if err := manager.Refresh(home); err != nil {
		t.Fatal(err)
	}
	if err := manager.MarkReady(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Select("work"); err != nil {
		t.Fatal(err)
	}
	if got := manager.Snapshot(); got.Active != "web" || got.Selected != "work" {
		t.Fatalf("pending switch overwrote active profile: %+v", got)
	}
	manager.ClearActive()
	loaded := New(path)
	loaded.Refresh(home)
	if loaded.Snapshot().Selected != "web" {
		t.Fatal("failed/pending switch was persisted")
	}
	if err := manager.MarkReady(); err != nil {
		t.Fatal(err)
	}
	loaded = New(path)
	loaded.Refresh(home)
	if loaded.Snapshot().Selected != "work" {
		t.Fatal("successful switch was not persisted")
	}
	otherHome := t.TempDir()
	loaded.Refresh(otherHome)
	if loaded.Snapshot().Selected != Default {
		t.Fatal("selection leaked to a different DSH_HOME")
	}
}

func TestInvalidSavedSelectionFallsBackAndMissingSelectionIsRejected(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "profile.json")
	for _, data := range []string{`not-json`, `{"home":` + stringJSON(home) + `,"name":"missing"}`, `{"home":` + stringJSON(home) + `,"name":"../other"}`} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		manager := New(path)
		manager.Refresh(home)
		if manager.Snapshot().Selected != Default {
			t.Fatal("invalid preference did not fall back to web")
		}
		if err := manager.Select("missing"); err == nil {
			t.Fatal("missing custom profile was accepted")
		}
		if manager.Snapshot().Selected != Default {
			t.Fatal("invalid selection changed current profile")
		}
	}
}

func stringJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
