package dshenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetEnvironmentReplacesExistingValue(t *testing.T) {
	got := SetEnvironment([]string{"PATH=/bin", "DSH_HOME=old", "OTHER=value"}, "DSH_HOME", "new")
	want := []string{"PATH=/bin", "OTHER=value", "DSH_HOME=new"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("SetEnvironment() = %#v, want %#v", got, want)
	}
}

func TestPrependExecutablePathsDeduplicatesEntries(t *testing.T) {
	first := filepath.Join("", "opt", "homebrew", "bin")
	existing := filepath.Join("", "usr", "bin")
	environment := []string{"OTHER=value", "PATH=" + existing}
	got := PrependExecutablePaths(environment, first, first)
	want := strings.Join([]string{first, existing}, string(os.PathListSeparator))
	if value := EnvironmentValue(got, "PATH"); value != want {
		t.Fatalf("PATH = %q, want %q", value, want)
	}
}

func TestFindNodeUsesPreparedPath(t *testing.T) {
	directory := t.TempDir()
	node := filepath.Join(directory, executableName("node"))
	if err := os.WriteFile(node, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := FindNode([]string{"PATH=" + directory})
	if err != nil {
		t.Fatalf("FindNode() error = %v", err)
	}
	if path != node {
		t.Fatalf("FindNode() = %q, want %q", path, node)
	}
}
