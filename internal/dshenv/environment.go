// Package dshenv resolves the environment, executables, and workspace needed
// to start DSH from a desktop application with a limited GUI environment.
package dshenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrPackageRunnerNotFound = errors.New("neither bunx nor npx was found")
	ErrNodeNotFound          = errors.New("Node.js was not found")
	ErrWorkspace             = errors.New("DSH workspace is invalid")
)

// PackageRunner is the executable selected to download and run DSH.
type PackageRunner struct {
	Name string
	Path string
}

// Runtime contains the complete environment and paths used to launch DSH.
type Runtime struct {
	Environment []string
	Runner      PackageRunner
	NodePath    string
	DSHHome     string
	Workspace   string
	RegistryURL string
	Shell       ShellImport
	ShellError  error
}

// Resolve builds one consistent DSH runtime from the platform environment.
// Shell loading is best-effort; executable and workspace failures are fatal.
func Resolve(base []string) (Runtime, error) {
	environment, shell, shellErr := LoadShellEnvironment(base)
	result := Runtime{Environment: environment, Shell: shell, ShellError: shellErr}

	runner, err := FindPackageRunner(environment)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrPackageRunnerNotFound, err)
	}
	nodePath, err := FindNode(environment)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrNodeNotFound, err)
	}
	environment, dshHome := withDSHHome(environment)
	if runner.Name == RunnerNPX {
		environment = SetEnvironment(environment, "NPM_CONFIG_YES", "true")
	}
	environment = PrependExecutablePaths(
		environment,
		runtimeExecutablePaths(environment, runner.Path, nodePath)...,
	)
	workspace, err := Workspace(environment)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrWorkspace, err)
	}

	result.Environment = environment
	result.Runner = runner
	result.NodePath = nodePath
	result.DSHHome = dshHome
	result.Workspace = workspace
	result.RegistryURL = npmRegistryURL(environment)
	return result, nil
}

func npmRegistryURL(environment []string) string {
	for _, name := range []string{"DSH_NPM_REGISTRY", "NPM_CONFIG_REGISTRY"} {
		if value := strings.TrimSpace(EnvironmentValue(environment, name)); value != "" {
			return value
		}
	}
	return ""
}

// Workspace resolves DSH_WORKSPACE or falls back to the prepared HOME.
func Workspace(environment []string) (string, error) {
	if configured := strings.TrimSpace(EnvironmentValue(environment, "DSH_WORKSPACE")); configured != "" {
		path, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("invalid DSH_WORKSPACE: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("DSH_WORKSPACE is not a directory: %s", path)
		}
		return path, nil
	}
	return homeDirectory(environment)
}

func withDSHHome(environment []string) ([]string, string) {
	if configured := strings.TrimSpace(EnvironmentValue(environment, "DSH_HOME")); configured != "" {
		return environment, configured
	}
	if configHome := strings.TrimSpace(EnvironmentValue(environment, "XDG_CONFIG_HOME")); configHome != "" {
		if dshHome := existingDirectory(filepath.Join(configHome, "dsh")); dshHome != "" {
			return SetEnvironment(environment, "DSH_HOME", dshHome), dshHome
		}
	}
	home, err := homeDirectory(environment)
	if err != nil {
		return environment, ""
	}
	dshHome := existingDirectory(filepath.Join(home, ".config", "dsh"))
	if dshHome == "" {
		return environment, ""
	}
	return SetEnvironment(environment, "DSH_HOME", dshHome), dshHome
}

func existingDirectory(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

// SetEnvironment replaces key with one value while preserving other entries.
func SetEnvironment(environment []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		comparison := item
		if runtime.GOOS == "windows" {
			comparison = strings.ToUpper(item)
		}
		if strings.HasPrefix(comparison, prefix) {
			continue
		}
		result = append(result, item)
	}
	return append(result, key+"="+value)
}

// EnvironmentValue returns one case-insensitive environment value.
func EnvironmentValue(environment []string, key string) string {
	for _, item := range environment {
		name, value, found := strings.Cut(item, "=")
		if found && strings.EqualFold(name, key) {
			return value
		}
	}
	return ""
}

// PrependExecutablePaths prepends unique executable paths to PATH.
func PrependExecutablePaths(environment []string, paths ...string) []string {
	existing := filepath.SplitList(EnvironmentValue(environment, "PATH"))
	combined := make([]string, 0, len(paths)+len(existing))
	seen := make(map[string]struct{}, len(paths)+len(existing))
	for _, path := range append(paths, existing...) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		key := filepath.Clean(path)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		combined = append(combined, path)
	}
	return SetEnvironment(environment, "PATH", strings.Join(combined, string(os.PathListSeparator)))
}

func homeDirectory(environment []string) (string, error) {
	for _, name := range []string{"HOME", "USERPROFILE"} {
		if home := strings.TrimSpace(EnvironmentValue(environment, name)); home != "" {
			return home, nil
		}
	}
	return os.UserHomeDir()
}
