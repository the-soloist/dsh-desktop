package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestScheduleDueAndMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "update-check.json")
	schedule := NewSchedule(path, 72*time.Hour)
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	if !schedule.Due(now) {
		t.Fatal("new schedule should be due")
	}
	if err := schedule.Mark(now); err != nil {
		t.Fatal(err)
	}
	if schedule.Due(now.Add(71*time.Hour + 59*time.Minute)) {
		t.Fatal("schedule should not be due before interval")
	}
	if !schedule.Due(now.Add(72 * time.Hour)) {
		t.Fatal("schedule should be due at interval")
	}
	if runtime.GOOS != "windows" {
		mode, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode.Mode().Perm() != 0o600 {
			t.Fatalf("schedule permissions = %o, want 600", mode.Mode().Perm())
		}
	}
}

func TestScheduleMalformedStateIsDue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	schedule := NewSchedule(path, 72*time.Hour)
	if !schedule.Due(time.Now()) {
		t.Fatal("malformed schedule should be due")
	}
}
