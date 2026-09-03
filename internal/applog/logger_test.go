package applog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotatingWriterBoundsLogHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.log")
	writer, err := newRotatingWriter(path, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"first", "second", "third", "fourth"} {
		if _, err = writer.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		if _, err = os.Stat(candidate); err != nil {
			t.Fatalf("expected rotated log %s: %v", candidate, err)
		}
	}
	if _, err = os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra backup: %v", err)
	}
}
