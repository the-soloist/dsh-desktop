//go:build !windows

package backend

import (
	"io"
	"log"
	"os/exec"
	"testing"
	"time"
)

func TestStopCurrentTerminatesProcessGroup(t *testing.T) {
	command := exec.Command("sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done")
	configureChildProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := &Process{cmd: command, done: make(chan struct{})}
	go process.wait()
	supervisor := NewSupervisor(Config{Logger: log.New(io.Discard, "", 0), StopTimeout: 2 * time.Second})
	supervisor.active = process
	t.Cleanup(func() { _ = supervisor.Close() })
	if err := supervisor.StopCurrent(); err != nil {
		t.Fatalf("StopCurrent() error = %v", err)
	}
	select {
	case <-process.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("process group did not terminate")
	}
}
