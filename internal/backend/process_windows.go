//go:build windows

package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

func newPackageRunnerCommand(ctx context.Context, runnerPath string, arguments ...string) *exec.Cmd {
	extension := strings.ToLower(filepath.Ext(runnerPath))
	if extension != ".cmd" && extension != ".bat" {
		return exec.CommandContext(ctx, runnerPath, arguments...)
	}
	parts := make([]string, 0, len(arguments)+1)
	parts = append(parts, quoteCommandPromptArgument(runnerPath))
	for _, argument := range arguments {
		parts = append(parts, quoteCommandPromptArgument(argument))
	}
	return exec.CommandContext(ctx, "cmd.exe", "/D", "/S", "/C", strings.Join(parts, " "))
}

func quoteCommandPromptArgument(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, `"`, `""`)
	return `"` + value + `"`
}

func configureChildProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNoWindow,
		HideWindow:    true,
	}
}

func terminateProcessTree(processID int, force bool) error {
	arguments := []string{"/PID", strconv.Itoa(processID), "/T"}
	if force {
		arguments = append(arguments, "/F")
	}
	command := exec.Command("taskkill.exe", arguments...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	if err := command.Run(); err != nil {
		return fmt.Errorf("taskkill: %w", err)
	}
	return nil
}
