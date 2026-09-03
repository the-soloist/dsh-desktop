// Package dshenv resolves the executables, environment, and workspace needed
// to start DSH from a desktop application with a limited GUI PATH.
package dshenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FindBunx locates bunx in PATH and common per-user installation locations.
func FindBunx() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DSH_BUNX_PATH")); configured != "" {
		path, err := filepath.Abs(configured)
		if err == nil && isExecutableFile(path) {
			return path, nil
		}
	}
	if path, err := exec.LookPath("bunx"); err == nil {
		return filepath.Abs(path)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	candidates := []string{filepath.Join(home, ".bun", "bin", executableName("bunx"))}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			filepath.Join("/opt/homebrew/bin", executableName("bunx")),
			filepath.Join("/usr/local/bin", executableName("bunx")),
		)
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

// BuildEnvironment adds DSH_HOME when the user's XDG dsh directory exists and
// restores common executable paths omitted from GUI application environments.
func BuildEnvironment(base []string, bunxPath string) ([]string, string) {
	environment, dshHome := withDSHHome(base)
	return PrependExecutablePaths(environment, executableSearchPaths(bunxPath)...), dshHome
}

// FindNode locates the Node.js interpreter in the prepared child environment.
func FindNode(environment []string) (string, error) {
	for _, directory := range filepath.SplitList(EnvironmentValue(environment, "PATH")) {
		candidate := filepath.Join(directory, executableName("node"))
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("node: %w", exec.ErrNotFound)
}

// Workspace resolves DSH_WORKSPACE or falls back to the user's home directory.
func Workspace() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DSH_WORKSPACE")); configured != "" {
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
	return os.UserHomeDir()
}

func withDSHHome(base []string) ([]string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return base, ""
	}
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	dshHome := filepath.Join(configHome, "dsh")
	if info, err := os.Stat(dshHome); err != nil || !info.IsDir() {
		return base, ""
	}
	return SetEnvironment(base, "DSH_HOME", dshHome), dshHome
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

func executableSearchPaths(bunxPath string) []string {
	paths := []string{filepath.Dir(bunxPath)}
	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths,
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".local", "share", "fnm", "aliases", "default", "bin"),
			filepath.Join(home, ".nvm", "current", "bin"),
		)
	}
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, "/opt/homebrew/bin", "/usr/local/bin")
	case "linux":
		if homebrewPrefix := strings.TrimSpace(os.Getenv("HOMEBREW_PREFIX")); homebrewPrefix != "" {
			paths = append(paths, filepath.Join(homebrewPrefix, "bin"))
		}
		if home != "" {
			paths = append(paths, filepath.Join(home, ".linuxbrew", "bin"))
		}
		paths = append(paths, "/home/linuxbrew/.linuxbrew/bin", "/usr/local/bin", "/usr/bin", "/snap/bin")
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root != "" {
				paths = append(paths, filepath.Join(root, "nodejs"))
			}
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			paths = append(paths, filepath.Join(localAppData, "Programs", "nodejs"))
		}
	}
	return paths
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}
