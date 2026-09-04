//go:build windows

package dshenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindNPXSupportsWindowsCommandScript(t *testing.T) {
	directory := t.TempDir()
	npx := filepath.Join(directory, "npx.cmd")
	if err := os.WriteFile(npx, []byte("@echo off"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := FindNPX([]string{"PATH=" + directory})
	if err != nil {
		t.Fatalf("FindNPX() error = %v", err)
	}
	if path != npx {
		t.Fatalf("FindNPX() = %q, want %q", path, npx)
	}
}
