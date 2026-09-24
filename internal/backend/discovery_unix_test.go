//go:build !windows

package backend

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestParsePSProcessesIgnoresZombiesAndPreservesIdentity(t *testing.T) {
	processes := parsePSProcesses("  10 1 Thu Sep 24 10:00:00 2026 S node /cache/@deepseek-ai/dsh/cli.js web --port 9000\n" +
		"  11 1 Thu Sep 24 10:00:00 2026 Z node <defunct>\n")
	if len(processes) != 1 || processes[0].PID != 10 || processes[0].ParentPID != 1 || processes[0].Started != "Thu Sep 24 10:00:00 2026" || !isDSHCommand(processes[0].Command) {
		t.Fatalf("parsed processes = %#v", processes)
	}
}

func TestDiscoverAndStopOnlyOwnedFixtureProcess(t *testing.T) {
	// Give a harmless sleep child the DSH argv[0]. Only this exact PID is ever
	// passed to StopDSHProcesses; actual user processes are never stopped.
	command := exec.Command("sleep", "60")
	command.Args[0] = "dsh"
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	processes, err := FindDSHProcesses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, process := range processes {
		if process.PID == command.Process.Pid {
			if err := StopDSHProcesses(ctx, []DSHProcess{process}); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("did not discover the fixture DSH process")
}
