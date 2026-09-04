//go:build !windows

package backend

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

func newPackageRunnerCommand(ctx context.Context, runnerPath string, arguments ...string) *exec.Cmd {
	return exec.CommandContext(ctx, runnerPath, arguments...)
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
