//go:build !windows

package backend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func snapshotProcesses(ctx context.Context) ([]processIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ps", "-axww", "-o", "pid=,ppid=,lstart=,stat=,command=")
	output, err := command.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("inspect processes: %w", err)
	}
	return parsePSProcesses(string(output)), nil
}

func parsePSProcesses(output string) []processIdentity {
	var result []processIdentity
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 || strings.HasPrefix(fields[7], "Z") {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		if pidErr != nil || parentErr != nil {
			continue
		}
		result = append(result, processIdentity{PID: pid, ParentPID: parent,
			Started: strings.Join(fields[2:7], " "), Command: strings.Join(fields[8:], " ")})
	}
	return result
}

func signalExternalProcess(ctx context.Context, target processIdentity, force bool) error {
	current, err := snapshotProcesses(ctx)
	if err != nil {
		return err
	}
	for _, process := range current {
		if !target.same(process) {
			continue
		}
		signal := syscall.SIGTERM
		if force {
			signal = syscall.SIGKILL
		}
		err := syscall.Kill(target.PID, signal)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}
