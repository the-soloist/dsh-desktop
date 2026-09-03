//go:build darwin || linux

package dshenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const shellEnvironmentTimeout = 3 * time.Second

func loadShellEnvironment(base []string) ([]string, ShellImport, error) {
	environment := append([]string(nil), base...)
	shell, kind, err := resolveShell(base)
	if err != nil {
		return environment, ShellImport{}, err
	}
	sources, err := shellSources(kind, base)
	if err != nil || len(sources) == 0 {
		return environment, ShellImport{Shell: shell, Sources: sources}, err
	}

	commandEnvironment := append([]string(nil), base...)
	if EnvironmentValue(commandEnvironment, "HOME") == "" {
		if home, homeErr := homeDirectory(base); homeErr == nil {
			commandEnvironment = SetEnvironment(commandEnvironment, "HOME", home)
		}
	}
	if EnvironmentValue(commandEnvironment, "SHELL") == "" {
		commandEnvironment = SetEnvironment(commandEnvironment, "SHELL", shell)
	}
	ctx, cancel := context.WithTimeout(context.Background(), shellEnvironmentTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, shell, shellInvocation(kind, sources)...)
	command.Env = commandEnvironment
	output, err := command.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return environment, ShellImport{Shell: shell, Sources: sources}, fmt.Errorf("loading %s environment timed out", kind)
		}
		return environment, ShellImport{Shell: shell, Sources: sources}, fmt.Errorf("load %s environment: %w", kind, err)
	}
	marker := append([]byte{0}, []byte("DSH_DESKTOP_ENV\x00")...)
	_, values, found := bytes.Cut(output, marker)
	if !found {
		return environment, ShellImport{Shell: shell, Sources: sources}, errors.New("shell environment output marker was not found")
	}

	imported := make([]string, 0, 16)
	for _, item := range strings.Split(string(values), "\x00") {
		name, value, found := strings.Cut(item, "=")
		if !found || !validEnvironmentName(name) {
			continue
		}
		current := EnvironmentValue(base, name)
		if current != "" && name != "PATH" {
			continue
		}
		if current == value {
			continue
		}
		environment = SetEnvironment(environment, name, value)
		imported = append(imported, name)
	}
	sort.Strings(imported)
	return environment, ShellImport{Shell: shell, Sources: sources, Variables: imported}, nil
}

func resolveShell(environment []string) (string, string, error) {
	configured := strings.TrimSpace(EnvironmentValue(environment, "SHELL"))
	if configured != "" {
		kind := shellKind(configured)
		if kind != "" {
			if isExecutableFile(configured) {
				return configured, kind, nil
			}
			if path, err := exec.LookPath(configured); err == nil {
				return path, kind, nil
			}
		}
	}
	candidates := []string{"/bin/bash", "/bin/zsh", "/bin/fish", "/bin/sh"}
	if runtime.GOOS == "darwin" {
		candidates[0], candidates[1] = candidates[1], candidates[0]
	}
	for _, candidate := range candidates {
		if kind := shellKind(candidate); kind != "" && isExecutableFile(candidate) {
			return candidate, kind, nil
		}
	}
	return "", "", errors.New("no supported shell was found")
}

func shellKind(shell string) string {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(shell)), ".exe")
	switch name {
	case "zsh", "bash", "fish", "sh", "dash", "ksh", "mksh":
		return name
	default:
		return ""
	}
}

func shellSources(kind string, environment []string) ([]string, error) {
	home, err := homeDirectory(environment)
	if err != nil {
		return nil, err
	}
	var candidates []string
	switch kind {
	case "zsh":
		directory := strings.TrimSpace(EnvironmentValue(environment, "ZDOTDIR"))
		if directory == "" {
			directory = home
		}
		candidates = []string{
			filepath.Join(directory, ".zshenv"),
			filepath.Join(directory, ".zprofile"),
			filepath.Join(directory, ".zlogin"),
		}
	case "bash":
		for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
			candidate := filepath.Join(home, name)
			if regularFile(candidate) {
				candidates = append(candidates, candidate)
				break
			}
		}
		if len(candidates) == 0 {
			candidates = append(candidates, filepath.Join(home, ".bashrc"))
		}
	case "fish":
		configHome := strings.TrimSpace(EnvironmentValue(environment, "XDG_CONFIG_HOME"))
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		candidates = []string{filepath.Join(configHome, "fish", "config.fish")}
	default:
		candidates = []string{filepath.Join(home, ".profile")}
	}

	sources := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if regularFile(candidate) {
			sources = append(sources, candidate)
		}
	}
	return sources, nil
}

func shellInvocation(kind string, sources []string) []string {
	if kind == "fish" {
		script := `for file in $argv; source "$file" >/dev/null 2>&1; end; printf '\000DSH_DESKTOP_ENV\000'; /usr/bin/env -0`
		return append([]string{"--no-config", "-c", script}, sources...)
	}
	script := `for file do . "$file" >/dev/null 2>&1; done; printf '\000DSH_DESKTOP_ENV\000'; /usr/bin/env -0`
	var arguments []string
	switch kind {
	case "zsh":
		arguments = []string{"-d", "-f", "-c", script, "dsh-desktop"}
	case "bash":
		arguments = []string{"--noprofile", "--norc", "-c", script, "dsh-desktop"}
	default:
		arguments = []string{"-c", script, "dsh-desktop"}
	}
	return append(arguments, sources...)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character == '_' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' {
			continue
		}
		if index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}
