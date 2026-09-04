package dshenv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	RunnerBunx = "bunx"
	RunnerNPX  = "npx"
)

// FindPackageRunner prefers bunx and falls back to npx only when bunx is not
// installed. Invalid explicit configuration remains an error.
func FindPackageRunner(environment []string) (PackageRunner, error) {
	return findPackageRunner(environment, FindBunx, FindNPX)
}

func findPackageRunner(
	environment []string,
	findBunx func([]string) (string, error),
	findNPX func([]string) (string, error),
) (PackageRunner, error) {
	path, err := findBunx(environment)
	if err == nil {
		return PackageRunner{Name: RunnerBunx, Path: path}, nil
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return PackageRunner{}, err
	}
	path, err = findNPX(environment)
	if err == nil {
		return PackageRunner{Name: RunnerNPX, Path: path}, nil
	}
	return PackageRunner{}, err
}

// FindBunx resolves bunx using tool-specific variables, XDG paths, PATH, and
// finally conventional platform paths, in that order.
func FindBunx(environment []string) (string, error) {
	if path, configured, err := configuredExecutable(environment, "DSH_BUNX_PATH"); configured {
		return path, err
	}
	if path := executableInDirectories("bunx", bunToolPaths(environment)); path != "" {
		return path, nil
	}
	if path := executableInDirectories("bunx", xdgExecutablePaths(environment)); path != "" {
		return path, nil
	}
	if path := executableInPath(environment, "bunx"); path != "" {
		return path, nil
	}
	if path := executableInDirectories("bunx", defaultBunPaths(environment)); path != "" {
		return path, nil
	}
	return "", exec.ErrNotFound
}

// FindNPX resolves npx using tool-specific variables, Node.js manager paths,
// XDG paths, PATH, and conventional platform paths, in that order.
func FindNPX(environment []string) (string, error) {
	if path, configured, err := configuredExecutable(environment, "DSH_NPX_PATH"); configured {
		return path, err
	}
	if path := executableInDirectories("npx", npmToolPaths(environment)); path != "" {
		return path, nil
	}
	if path := executableInDirectories("npx", xdgExecutablePaths(environment)); path != "" {
		return path, nil
	}
	if path := executableInPath(environment, "npx"); path != "" {
		return path, nil
	}
	if path := executableInDirectories("npx", defaultNodePaths(environment)); path != "" {
		return path, nil
	}
	return "", exec.ErrNotFound
}

// FindNode resolves Node.js using tool-specific variables, XDG paths, PATH,
// and finally conventional platform paths, in that order.
func FindNode(environment []string) (string, error) {
	if path, configured, err := configuredExecutable(environment, "DSH_NODE_PATH"); configured {
		return path, err
	}
	if path := executableInDirectories("node", nodeToolPaths(environment)); path != "" {
		return path, nil
	}
	if path := executableInDirectories("node", xdgExecutablePaths(environment)); path != "" {
		return path, nil
	}
	if path := executableInPath(environment, "node"); path != "" {
		return path, nil
	}
	if path := executableInDirectories("node", defaultNodePaths(environment)); path != "" {
		return path, nil
	}
	return "", exec.ErrNotFound
}

func runtimeExecutablePaths(environment []string, runnerPath, nodePath string) []string {
	paths := []string{filepath.Dir(runnerPath), filepath.Dir(nodePath)}
	paths = append(paths, bunToolPaths(environment)...)
	paths = append(paths, npmToolPaths(environment)...)
	paths = append(paths, xdgExecutablePaths(environment)...)
	paths = append(paths, defaultBunPaths(environment)...)
	paths = append(paths, defaultNodePaths(environment)...)
	return paths
}

func npmToolPaths(environment []string) []string {
	paths := nodeToolPaths(environment)
	if root := strings.TrimSpace(EnvironmentValue(environment, "NPM_CONFIG_PREFIX")); root != "" {
		if runtime.GOOS == "windows" {
			paths = append([]string{root}, paths...)
		} else {
			paths = append([]string{filepath.Join(root, "bin")}, paths...)
		}
	}
	return paths
}

func bunToolPaths(environment []string) []string {
	paths := make([]string, 0, 1)
	if root := strings.TrimSpace(EnvironmentValue(environment, "BUN_INSTALL")); root != "" {
		paths = append(paths, filepath.Join(root, "bin"))
	}
	return paths
}

func nodeToolPaths(environment []string) []string {
	paths := make([]string, 0, 8)
	for _, item := range []struct {
		name   string
		suffix string
	}{
		{name: "NODE_HOME", suffix: "bin"},
		{name: "FNM_MULTISHELL_PATH"},
		{name: "NVM_BIN"},
		{name: "VOLTA_HOME", suffix: "bin"},
		{name: "ASDF_DATA_DIR", suffix: "shims"},
		{name: "MISE_DATA_DIR", suffix: "shims"},
		{name: "PNPM_HOME"},
	} {
		if root := strings.TrimSpace(EnvironmentValue(environment, item.name)); root != "" {
			paths = append(paths, filepath.Join(root, item.suffix))
		}
	}
	return paths
}

func xdgExecutablePaths(environment []string) []string {
	paths := make([]string, 0, 8)
	if root := strings.TrimSpace(EnvironmentValue(environment, "XDG_BIN_HOME")); root != "" {
		paths = append(paths, root)
	}
	if root := strings.TrimSpace(EnvironmentValue(environment, "XDG_DATA_HOME")); root != "" {
		paths = append(paths,
			filepath.Clean(filepath.Join(root, "..", "bin")),
			filepath.Join(root, "bun", "bin"),
			filepath.Join(root, "fnm", "aliases", "default", "bin"),
			filepath.Join(root, "nvm", "current", "bin"),
			filepath.Join(root, "volta", "bin"),
			filepath.Join(root, "mise", "shims"),
		)
	}
	return paths
}

func defaultBunPaths(environment []string) []string {
	paths := make([]string, 0, 4)
	if home, err := homeDirectory(environment); err == nil {
		paths = append(paths, filepath.Join(home, ".bun", "bin"))
	}
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, "/opt/homebrew/bin", "/usr/local/bin")
	case "linux":
		paths = append(paths, "/home/linuxbrew/.linuxbrew/bin", "/usr/local/bin", "/usr/bin", "/snap/bin")
	}
	return paths
}

func defaultNodePaths(environment []string) []string {
	paths := make([]string, 0, 12)
	if home, err := homeDirectory(environment); err == nil {
		paths = append(paths,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".local", "share", "fnm", "aliases", "default", "bin"),
			filepath.Join(home, ".nvm", "current", "bin"),
			filepath.Join(home, ".volta", "bin"),
		)
		if runtime.GOOS == "linux" {
			paths = append(paths, filepath.Join(home, ".linuxbrew", "bin"))
		}
	}
	return append(paths, platformExecutablePaths(environment)...)
}

func platformExecutablePaths(environment []string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"}
	case "linux":
		paths := make([]string, 0, 6)
		if prefix := strings.TrimSpace(EnvironmentValue(environment, "HOMEBREW_PREFIX")); prefix != "" {
			paths = append(paths, filepath.Join(prefix, "bin"))
		}
		return append(paths, "/home/linuxbrew/.linuxbrew/bin", "/usr/local/bin", "/usr/bin", "/snap/bin")
	case "windows":
		paths := make([]string, 0, 3)
		for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if root := strings.TrimSpace(EnvironmentValue(environment, name)); root != "" {
				paths = append(paths, filepath.Join(root, "nodejs"))
			}
		}
		if root := strings.TrimSpace(EnvironmentValue(environment, "LOCALAPPDATA")); root != "" {
			paths = append(paths, filepath.Join(root, "Programs", "nodejs"))
		}
		return paths
	default:
		return nil
	}
}

func configuredExecutable(environment []string, variable string) (string, bool, error) {
	configured := strings.TrimSpace(EnvironmentValue(environment, variable))
	if configured == "" {
		return "", false, nil
	}
	path, err := filepath.Abs(configured)
	if err != nil {
		return "", true, fmt.Errorf("invalid %s: %w", variable, err)
	}
	if !isExecutableFile(path) {
		return "", true, fmt.Errorf("%s does not point to an executable file: %s", variable, path)
	}
	return path, true, nil
}

func executableInPath(environment []string, name string) string {
	return executableInDirectories(name, filepath.SplitList(EnvironmentValue(environment, "PATH")))
}

func executableInDirectories(name string, directories []string) string {
	for _, directory := range directories {
		if strings.TrimSpace(directory) == "" {
			continue
		}
		for _, filename := range executableNames(name) {
			candidate := filepath.Join(directory, filename)
			if !isExecutableFile(candidate) {
				continue
			}
			path, err := filepath.Abs(candidate)
			if err == nil {
				return path
			}
		}
	}
	return ""
}

func executableNames(name string) []string {
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		return []string{name + ".exe", name + ".cmd", name + ".bat", name + ".com"}
	}
	return []string{name}
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
