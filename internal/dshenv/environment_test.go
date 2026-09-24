package dshenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestFindPackageRunnerPrefersBunx(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"bunx", "npx"} {
		if err := os.WriteFile(filepath.Join(directory, executableName(name)), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runner, err := FindPackageRunner([]string{"PATH=" + directory})
	if err != nil {
		t.Fatalf("FindPackageRunner() error = %v", err)
	}
	if runner.Name != RunnerBunx || runner.Path != filepath.Join(directory, executableName("bunx")) {
		t.Fatalf("runner = %#v, want bunx", runner)
	}
}

func TestFindPackageRunnerFallsBackToNPX(t *testing.T) {
	npx := filepath.Join(t.TempDir(), executableName("npx"))
	runner, err := findPackageRunner(
		nil,
		func([]string) (string, error) { return "", exec.ErrNotFound },
		func([]string) (string, error) { return npx, nil },
	)
	if err != nil {
		t.Fatalf("FindPackageRunner() error = %v", err)
	}
	if runner.Name != RunnerNPX || runner.Path != npx {
		t.Fatalf("runner = %#v, want npx at %q", runner, npx)
	}
}

func TestFindNPXPrefersToolConfigurationToXDGAndPath(t *testing.T) {
	home := t.TempDir()
	npmPrefix := filepath.Join(home, "npm")
	xdgBin := filepath.Join(home, "xdg-bin")
	pathBin := filepath.Join(home, "path-bin")
	npmBin := npmPrefix
	if executableName("npx") == "npx" {
		npmBin = filepath.Join(npmPrefix, "bin")
	}
	for _, directory := range []string{npmBin, xdgBin, pathBin} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, executableName("npx")), []byte("npx"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path, err := FindNPX([]string{
		"HOME=" + home,
		"NPM_CONFIG_PREFIX=" + npmPrefix,
		"XDG_BIN_HOME=" + xdgBin,
		"PATH=" + pathBin,
	})
	if err != nil {
		t.Fatalf("FindNPX() error = %v", err)
	}
	want := filepath.Join(npmBin, executableName("npx"))
	if path != want {
		t.Fatalf("FindNPX() = %q, want npm-configured path %q", path, want)
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
	resolved, err := Resolve(runtimeTestEnvironment(t, home))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Runner.Name != RunnerBunx {
		t.Errorf("runner name = %q, want %q", resolved.Runner.Name, RunnerBunx)
	}
	assertSamePath(t, "runner", resolved.Runner.Path, filepath.Join(home, "bun", "bin", executableName("bunx")))
	assertSamePath(t, "node", resolved.NodePath, filepath.Join(home, "node", "bin", executableName("node")))
	assertSamePath(t, "DSH_HOME", resolved.DSHHome, filepath.Join(home, "xdg-config", "dsh"))
	assertSamePath(t, "workspace", resolved.Workspace, filepath.Join(home, "workspace"))
	path := filepath.SplitList(EnvironmentValue(resolved.Environment, "PATH"))
	if len(path) < 2 {
		t.Fatalf("resolved PATH = %#v, want bun and node directories first", path)
	}
	assertSamePath(t, "PATH[0]", path[0], filepath.Join(home, "bun", "bin"))
	assertSamePath(t, "PATH[1]", path[1], filepath.Join(home, "node", "bin"))
	for _, check := range []struct {
		name, got, want string
	}{
		{"DSH_HOME", EnvironmentValue(resolved.Environment, "DSH_HOME"), resolved.DSHHome},
		{"DSH_WORKSPACE", EnvironmentValue(resolved.Environment, "DSH_WORKSPACE"), resolved.Workspace},
		{"PATH[0]", path[0], filepath.Dir(resolved.Runner.Path)},
		{"PATH[1]", path[1], filepath.Dir(resolved.NodePath)},
	} {
		if check.got != check.want {
			t.Errorf("environment %s = %q, want runtime value %q", check.name, check.got, check.want)
		}
	}
}

// Runtime paths can have different spellings after Windows short-name expansion.
// Compare file identity without using the production normalizer as the oracle.
func assertSamePath(t *testing.T, name, got, want string) {
	t.Helper()
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Errorf("%s = %q, want same file as %q: %v", name, got, want, err)
		return
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat expected %s path %q: %v", name, want, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("%s = %q, want same file as %q", name, got, want)
	}
}

func runtimeTestEnvironment(t *testing.T, home string) []string {
	t.Helper()
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

	return []string{
		"HOME=" + home,
		"PATH=",
		"BUN_INSTALL=" + bunInstall,
		"NODE_HOME=" + nodeHome,
		"XDG_CONFIG_HOME=" + xdgConfigHome,
		"DSH_WORKSPACE=" + workspace,
		"TMP=" + home,
		"TEMP=" + home,
	}
}

func TestResolveReadsConfiguredNPMRegistry(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bunx", "node"} {
		if err := os.WriteFile(filepath.Join(bin, executableName(name)), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := Resolve([]string{
		"HOME=" + home,
		"PATH=" + bin,
		"DSH_NPM_REGISTRY=https://registry.example.test/npm",
		"NPM_CONFIG_REGISTRY=https://ignored.example.test",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.RegistryURL != "https://registry.example.test/npm" {
		t.Fatalf("RegistryURL = %q", resolved.RegistryURL)
	}
}

func TestWithBunxTempEnvironmentFillsMissingVariable(t *testing.T) {
	got := withBunxTempEnvironment([]string{"TMP=/custom/tmp"})
	if EnvironmentValue(got, "TMP") != "/custom/tmp" || EnvironmentValue(got, "TEMP") != "/custom/tmp" {
		t.Fatalf("copied TEMP = TMP:%q TEMP:%q", EnvironmentValue(got, "TMP"), EnvironmentValue(got, "TEMP"))
	}
	got = withBunxTempEnvironment([]string{"TEMP=/custom/temp", "TMP=   "})
	if EnvironmentValue(got, "TMP") != "/custom/temp" || EnvironmentValue(got, "TEMP") != "/custom/temp" {
		t.Fatalf("copied TMP = TMP:%q TEMP:%q", EnvironmentValue(got, "TMP"), EnvironmentValue(got, "TEMP"))
	}
}

func TestWithBunxTempEnvironmentKeepsExistingValues(t *testing.T) {
	got := withBunxTempEnvironment([]string{"TMP=/one", "TEMP=/two", "OTHER=value"})
	if EnvironmentValue(got, "TMP") != "/one" || EnvironmentValue(got, "TEMP") != "/two" || EnvironmentValue(got, "OTHER") != "value" {
		t.Fatalf("environment = %#v", got)
	}
}

func TestWithBunxTempEnvironmentUsesPlatformDefault(t *testing.T) {
	directory := t.TempDir()
	got := withBunxTempEnvironment([]string{"TMPDIR=" + directory})
	if runtime.GOOS == "windows" {
		t.Skip("Windows default does not come from TMPDIR")
	}
	if EnvironmentValue(got, "TMP") != directory || EnvironmentValue(got, "TEMP") != directory {
		t.Fatalf("default temp = TMP:%q TEMP:%q, want %q", EnvironmentValue(got, "TMP"), EnvironmentValue(got, "TEMP"), directory)
	}
}

func TestDefaultTempDirectoryAdaptsToOperatingSystem(t *testing.T) {
	if got, want := defaultTempDirectory("windows", []string{"LOCALAPPDATA=C:\\Users\\me\\AppData\\Local"}), `C:\Users\me\AppData\Local\Temp`; got != want {
		t.Fatalf("LOCALAPPDATA temp = %q, want %q", got, want)
	}
	if got, want := defaultTempDirectory("windows", []string{"USERPROFILE=C:\\Users\\me"}), `C:\Users\me\AppData\Local\Temp`; got != want {
		t.Fatalf("USERPROFILE temp = %q, want %q", got, want)
	}
	if got, want := defaultTempDirectory("windows", nil), `C:\Windows\Temp`; got != want {
		t.Fatalf("windows fallback = %q, want %q", got, want)
	}
	if got, want := defaultTempDirectory("darwin", []string{"TMPDIR=/var/folders/T"}), "/var/folders/T"; got != want {
		t.Fatalf("darwin TMPDIR = %q, want %q", got, want)
	}
	if got, want := defaultTempDirectory("linux", nil), "/tmp"; got != want {
		t.Fatalf("linux fallback = %q, want %q", got, want)
	}
}
