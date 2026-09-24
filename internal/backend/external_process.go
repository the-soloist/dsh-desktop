package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// StopDSHProcesses stops only confirmed DSH identities and their descendants.
// It never derives a termination target from a port number.
func StopDSHProcesses(ctx context.Context, confirmed []DSHProcess) error {
	return stopDSHProcesses(ctx, confirmed, snapshotProcesses, signalExternalProcess)
}

func stopDSHProcesses(ctx context.Context, confirmed []DSHProcess,
	snapshot func(context.Context) ([]processIdentity, error),
	signal func(context.Context, processIdentity, bool) error,
) error {
	current, err := snapshot(ctx)
	if err != nil {
		return err
	}
	var targets []processIdentity
	for _, selected := range confirmed {
		if selected.PID <= 1 || selected.PID == os.Getpid() || selected.PID != selected.identity.PID || !isDSHCommand(selected.identity.Command) {
			return errors.New("invalid DSH process identity")
		}
		for _, process := range current {
			if process.PID != selected.PID {
				continue
			}
			if !process.same(selected.identity) {
				return fmt.Errorf("DSH process %d changed; inspect processes again", selected.PID)
			}
			targets = append(targets, process)
		}
	}
	// Capture descendants before stopping parents, while the ancestry is intact.
	seen := make(map[int]bool)
	for _, process := range targets {
		seen[process.PID] = true
	}
	for index := 0; index < len(targets); index++ {
		parent := targets[index]
		seen[parent.PID] = true
		for _, process := range current {
			if process.ParentPID == parent.PID && process.PID > 1 && !seen[process.PID] {
				seen[process.PID] = true
				targets = append(targets, process)
			}
		}
	}
	for _, force := range []bool{false, true} {
		for _, process := range targets {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := signal(ctx, process, force); err != nil {
				return fmt.Errorf("stop DSH process %d: %w", process.PID, err)
			}
		}
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		targets, err = waitForProcesses(waitCtx, targets, snapshot)
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if len(targets) == 0 {
			return nil
		}
	}
	return errors.New("DSH processes did not exit after termination")
}

func waitForProcesses(ctx context.Context, targets []processIdentity, snapshot func(context.Context) ([]processIdentity, error)) ([]processIdentity, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := snapshot(ctx)
		if err != nil {
			return targets, err
		}
		remaining := targets[:0]
		for _, target := range targets {
			for _, process := range current {
				if target.same(process) {
					remaining = append(remaining, target)
					break
				}
			}
		}
		targets = remaining
		if len(targets) == 0 {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return targets, ctx.Err()
		case <-ticker.C:
		}
	}
}
