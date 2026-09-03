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

func TestFindBunxPrefersXDGLocationToHomeFallback(t *testing.T) {
	home := t.TempDir()
	xdgDataHome := filepath.Join(home, "xdg", "share")
	xdgBinHome := filepath.Join(home, "xdg", "bin")
	for _, directory := range []string{xdgBinHome, filepath.Join(home, ".bun", "bin")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, executableName("bunx")), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	path, err := FindBunx([]string{
		"HOME=" + home,
		"PATH=",
		"XDG_BIN_HOME=" + xdgBinHome,
		"XDG_DATA_HOME=" + xdgDataHome,
	})
	if err != nil {
		t.Fatalf("FindBunx() error = %v", err)
	}
	want := filepath.Join(xdgBinHome, executableName("bunx"))
	if path != want {
		t.Fatalf("FindBunx() = %q, want XDG path %q", path, want)
	}
}

func TestFindBunxPrefersToolConfigurationToXDGAndPath(t *testing.T) {
	home := t.TempDir()
	bunInstall := filepath.Join(home, "tool-bun")
	xdgBin := filepath.Join(home, "xdg-bin")
	pathBin := filepath.Join(home, "path-bin")
	for _, directory := range []string{filepath.Join(bunInstall, "bin"), xdgBin, pathBin} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, executableName("bunx")), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path, err := FindBunx([]string{
		"HOME=" + home,
		"BUN_INSTALL=" + bunInstall,
		"XDG_BIN_HOME=" + xdgBin,
		"PATH=" + pathBin,
	})
	if err != nil {
		t.Fatalf("FindBunx() error = %v", err)
	}
	want := filepath.Join(bunInstall, "bin", executableName("bunx"))
	if path != want {
		t.Fatalf("FindBunx() = %q, want tool-configured path %q", path, want)
	}
}

func TestFindNodePrefersToolConfigurationToXDGAndPath(t *testing.T) {
	home := t.TempDir()
	nodeHome := filepath.Join(home, "tool-node")
	xdgBin := filepath.Join(home, "xdg-bin")
	pathBin := filepath.Join(home, "path-bin")
	for _, directory := range []string{filepath.Join(nodeHome, "bin"), xdgBin, pathBin} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, executableName("node")), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path, err := FindNode([]string{
		"HOME=" + home,
		"NODE_HOME=" + nodeHome,
		"XDG_BIN_HOME=" + xdgBin,
		"PATH=" + pathBin,
	})
	if err != nil {
		t.Fatalf("FindNode() error = %v", err)
	}
	want := filepath.Join(nodeHome, "bin", executableName("node"))
	if path != want {
		t.Fatalf("FindNode() = %q, want tool-configured path %q", path, want)
	}
}

func TestDSHHomePrefersToolConfigurationThenXDGThenDefault(t *testing.T) {
	home := t.TempDir()
	explicit := filepath.Join(home, "explicit-dsh")
	xdgHome := filepath.Join(home, "xdg-config")
	for _, directory := range []string{filepath.Join(xdgHome, "dsh"), filepath.Join(home, ".config", "dsh")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	environment, dshHome := withDSHHome([]string{
		"HOME=" + home,
		"DSH_HOME=" + explicit,
		"XDG_CONFIG_HOME=" + xdgHome,
	})
	if dshHome != explicit || EnvironmentValue(environment, "DSH_HOME") != explicit {
		t.Fatalf("DSH_HOME = %q, want explicit tool value %q", dshHome, explicit)
	}

	environment, dshHome = withDSHHome([]string{"HOME=" + home, "XDG_CONFIG_HOME=" + xdgHome})
	wantXDG := filepath.Join(xdgHome, "dsh")
	if dshHome != wantXDG || EnvironmentValue(environment, "DSH_HOME") != wantXDG {
		t.Fatalf("DSH_HOME = %q, want XDG value %q", dshHome, wantXDG)
	}
}

func TestResolveProducesOneRuntimeEnvironment(t *testing.T) {
	home := t.TempDir()
	bunInstall := filepath.Join(home, "bun")
	nodeHome := filepath.Join(home, "node")
	workspace := filepath.Join(home, "workspace")
	xdgConfigHome := filepath.Join(home, "xdg-config")
	for _, directory := range []string{
		filepath.Join(bunInstall, "bin"),
		filepath.Join(nodeHome, "bin"),
		workspace,
		filepath.Join(xdgConfigHome, "dsh"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, contents := range map[string]string{
		filepath.Join(bunInstall, "bin", executableName("bunx")): "bunx",
		filepath.Join(nodeHome, "bin", executableName("node")):   "node",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	resolved, err := Resolve([]string{
		"HOME=" + home,
		"PATH=",
		"BUN_INSTALL=" + bunInstall,
		"NODE_HOME=" + nodeHome,
		"XDG_CONFIG_HOME=" + xdgConfigHome,
		"DSH_WORKSPACE=" + workspace,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.BunxPath != filepath.Join(bunInstall, "bin", executableName("bunx")) {
		t.Errorf("bunx = %q", resolved.BunxPath)
	}
	if resolved.NodePath != filepath.Join(nodeHome, "bin", executableName("node")) {
		t.Errorf("node = %q", resolved.NodePath)
	}
	if resolved.DSHHome != filepath.Join(xdgConfigHome, "dsh") {
		t.Errorf("DSH_HOME = %q", resolved.DSHHome)
	}
	if resolved.Workspace != workspace {
		t.Errorf("workspace = %q", resolved.Workspace)
	}
	path := filepath.SplitList(EnvironmentValue(resolved.Environment, "PATH"))
	if len(path) < 2 || path[0] != filepath.Join(bunInstall, "bin") || path[1] != filepath.Join(nodeHome, "bin") {
		t.Errorf("resolved PATH = %#v", path)
	}
}
