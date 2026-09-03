//go:build darwin || linux

package dshenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadShellEnvironmentSupportsCommonShells(t *testing.T) {
	tests := []struct {
		name       string
		executable string
		config     string
		contents   string
	}{
		{
			name:       "zsh",
			executable: "zsh",
			config:     ".zshenv",
			contents:   posixTestEnvironment(),
		},
		{
			name:       "bash",
			executable: "bash",
			config:     ".bash_profile",
			contents:   posixTestEnvironment(),
		},
		{
			name:       "sh",
			executable: "sh",
			config:     ".profile",
			contents:   posixTestEnvironment(),
		},
		{
			name:       "fish",
			executable: "fish",
			config:     filepath.Join(".config", "fish", "config.fish"),
			contents: strings.Join([]string{
				`set -gx XDG_CONFIG_HOME "$HOME/config"`,
				`set -gx DSH_BUNX_PATH "$HOME/tools/bunx"`,
				`set -gx EXISTING "shell"`,
				`set -gx PATH "$HOME/bin" $PATH`,
			}, "\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shell, err := exec.LookPath(test.executable)
			if err != nil {
				t.Skipf("%s is not installed", test.executable)
			}
			home := t.TempDir()
			config := filepath.Join(home, test.config)
			if err = os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(config, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			base := []string{
				"HOME=" + home,
				"PATH=/usr/bin:/bin",
				"SHELL=" + shell,
				"EXISTING=process",
			}

			environment, imported, err := LoadShellEnvironment(base)
			if err != nil {
				t.Fatalf("LoadShellEnvironment() error = %v", err)
			}
			if value := EnvironmentValue(environment, "XDG_CONFIG_HOME"); value != filepath.Join(home, "config") {
				t.Errorf("XDG_CONFIG_HOME = %q", value)
			}
			if value := EnvironmentValue(environment, "DSH_BUNX_PATH"); value != filepath.Join(home, "tools", "bunx") {
				t.Errorf("DSH_BUNX_PATH = %q", value)
			}
			if value := EnvironmentValue(environment, "EXISTING"); value != "process" {
				t.Errorf("existing process variable was overwritten: %q", value)
			}
			if value := EnvironmentValue(environment, "PATH"); !strings.HasPrefix(value, filepath.Join(home, "bin")) {
				t.Errorf("PATH did not load shell value: %q", value)
			}
			if imported.Shell != shell || len(imported.Sources) != 1 || imported.Sources[0] != config {
				t.Errorf("shell import = %#v", imported)
			}
		})
	}
}

func posixTestEnvironment() string {
	return strings.Join([]string{
		`export XDG_CONFIG_HOME="$HOME/config"`,
		`export DSH_BUNX_PATH="$HOME/tools/bunx"`,
		`export EXISTING="shell"`,
		`export PATH="$HOME/bin:$PATH"`,
	}, "\n")
}
