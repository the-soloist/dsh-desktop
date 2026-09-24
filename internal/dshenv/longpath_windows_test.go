//go:build windows

package dshenv

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestResolveExpandsWindowsShortPaths(t *testing.T) {
	// Get the canonical fixture root from Windows, not the production normalizer.
	root := windowsTestPathName(t, t.TempDir(), windows.GetLongPathName)
	home := filepath.Join(root, "Runtime Environment With Long Names")
	base := runtimeTestEnvironment(t, home)
	shortHome := windowsTestPathName(t, home, windows.GetShortPathName)
	if strings.EqualFold(shortHome, home) {
		t.Skip("the temporary volume does not provide an 8.3 short-name alias")
	}
	t.Logf("resolving short home %q to %q", shortHome, home)
	assertSamePath(t, "short home", shortHome, home)
	for index, item := range base {
		base[index] = strings.ReplaceAll(item, home, shortHome)
	}

	resolved, err := Resolve(base)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Runner.Name != RunnerBunx {
		t.Errorf("runner name = %q, want %q", resolved.Runner.Name, RunnerBunx)
	}
	for _, check := range []struct {
		name, got, want string
	}{
		{"runner", resolved.Runner.Path, filepath.Join(home, "bun", "bin", "bunx.exe")},
		{"node", resolved.NodePath, filepath.Join(home, "node", "bin", "node.exe")},
		{"DSH_HOME", resolved.DSHHome, filepath.Join(home, "xdg-config", "dsh")},
		{"workspace", resolved.Workspace, filepath.Join(home, "workspace")},
	} {
		if check.got != check.want {
			t.Errorf("%s = %q, want long path %q", check.name, check.got, check.want)
		}
		assertSamePath(t, check.name, check.got, check.want)
	}
	for name, want := range map[string]string{
		"HOME":            home,
		"BUN_INSTALL":     filepath.Join(home, "bun"),
		"NODE_HOME":       filepath.Join(home, "node"),
		"XDG_CONFIG_HOME": filepath.Join(home, "xdg-config"),
		"DSH_HOME":        resolved.DSHHome,
		"DSH_WORKSPACE":   resolved.Workspace,
		"TMP":             home,
		"TEMP":            home,
	} {
		if got := EnvironmentValue(resolved.Environment, name); got != want {
			t.Errorf("environment %s = %q, want long path %q", name, got, want)
		}
	}
	path := filepath.SplitList(EnvironmentValue(resolved.Environment, "PATH"))
	if len(path) < 2 {
		t.Fatalf("resolved PATH = %#v, want bun and node directories first", path)
	}
	for index, want := range []string{filepath.Dir(resolved.Runner.Path), filepath.Dir(resolved.NodePath)} {
		if path[index] != want {
			t.Errorf("PATH[%d] = %q, want long path %q", index, path[index], want)
		}
	}
}

func windowsTestPathName(t *testing.T, path string, resolve func(*uint16, *uint16, uint32) (uint32, error)) string {
	t.Helper()
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	size, err := resolve(pointer, &buffer[0], uint32(len(buffer)))
	if err != nil || size == 0 || int(size) >= len(buffer) {
		t.Fatalf("resolve Windows path name %q: size = %d, error = %v", path, size, err)
	}
	return windows.UTF16ToString(buffer[:size])
}
