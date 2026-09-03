//go:build windows

package dshenv

import (
	"reflect"
	"testing"
)

func TestLoadShellEnvironmentUsesWindowsSystemEnvironment(t *testing.T) {
	base := []string{"PATH=C:\\Windows", "XDG_CONFIG_HOME=C:\\Config"}
	got, imported, err := LoadShellEnvironment(base)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("environment = %#v, want %#v", got, base)
	}
	if imported.Shell != "" || len(imported.Sources) != 0 || len(imported.Variables) != 0 {
		t.Fatalf("shell import = %#v, want empty", imported)
	}
	got[0] = "changed"
	if base[0] == "changed" {
		t.Fatal("Windows environment was not copied")
	}
}
