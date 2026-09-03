//go:build !windows

package desktop

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

func newBunxCommand(ctx context.Context, bunxPath string, arguments ...string) *exec.Cmd {
	return exec.CommandContext(ctx, bunxPath, arguments...)
}

func configureChildProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessTree(processID int, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-processID, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func ownsStartupConsole() bool {
	return false
}

func hideStartupConsole() {}
