//go:build !windows

package backend

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopCurrentTerminatesProcessGroup(t *testing.T) {
	output, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close(); _ = writer.Close() })
	// Both processes inherit writer. EOF proves that the descendant exited,
	// even on Linux runners where a terminated orphan can remain a zombie.
	command := exec.Command("sh", "-c", `trap 'exit 0' TERM; "$1" -test.run='^TestProcessGroupFixtureChild$' & wait "$!"`, "dsh-process-fixture", os.Args[0])
	command.Env = append(os.Environ(), "DSH_PROCESS_GROUP_FIXTURE=1")
	command.Stdout = writer
	configureChildProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := &Process{cmd: command, done: make(chan struct{})}
	go process.wait()
	childPID := 0
	stopped := false
	t.Cleanup(func() {
		// Do not depend on Supervisor.Close: StopCurrent releases ownership
		// before attempting termination, including on the failure path.
		if !stopped {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			if childPID > 1 {
				_ = syscall.Kill(childPID, syscall.SIGKILL)
			}
			_ = command.Process.Kill()
		}
		_ = output.Close()
		select {
		case <-process.Done():
		case <-time.After(5 * time.Second):
			t.Error("fixture parent could not be reaped")
		}
	})
	_ = writer.Close()
	ready := make(chan string, 1)
	closed := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(output)
		line, err := reader.ReadString('\n')
		ready <- line
		if err == nil {
			_, err = io.Copy(io.Discard, reader)
		}
		closed <- err
	}()
	select {
	case line := <-ready:
		childPID, err = strconv.Atoi(strings.TrimSpace(line))
		if err != nil || childPID <= 1 || childPID == command.Process.Pid {
			t.Fatalf("invalid fixture child PID %q: %v", line, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixture child did not report readiness")
	}
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("fixture child is not alive: %v", err)
	}
	if group, err := syscall.Getpgid(childPID); err != nil || group != command.Process.Pid {
		t.Fatalf("fixture child process group = %d, want %d: %v", group, command.Process.Pid, err)
	}
	supervisor := NewSupervisor(Config{Logger: log.New(io.Discard, "", 0), StopTimeout: 2 * time.Second})
	supervisor.active = process
	if err := supervisor.StopCurrent(); err != nil {
		t.Fatalf("StopCurrent() error = %v", err)
	}
	select {
	case <-process.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("parent did not terminate")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("read fixture output: %v", err)
		}
		stopped = true
	case <-time.After(5 * time.Second):
		t.Fatal("descendant still holds the output pipe after parent termination")
	}
}

func TestProcessGroupFixtureChild(t *testing.T) {
	if os.Getenv("DSH_PROCESS_GROUP_FIXTURE") != "1" {
		t.Skip("helper subprocess only")
	}
	// Report from the child after its runtime and signal handling are ready,
	// not from the shell immediately after fork (before exec has completed).
	fmt.Fprintln(os.Stdout, os.Getpid())
	time.Sleep(time.Minute)
	os.Exit(0)
}
