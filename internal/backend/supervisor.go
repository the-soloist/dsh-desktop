// Package backend owns the DSH child process and verifies the local web
// service before the desktop window navigates to it.
package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const responseBodyLimit = 64 * 1024

// ErrClosed is returned when a launch races with application shutdown.
var ErrClosed = errors.New("DSH supervisor is closed")

// ProbeStatus describes what is currently listening at the configured URL.
type ProbeStatus uint8

const (
	ProbeUnavailable ProbeStatus = iota
	ProbeReady
	ProbeUnexpected
)

// Config contains the process and readiness settings owned by a Supervisor.
type Config struct {
	Package            string
	URL                string
	PageMarker         string
	ReadinessInterval  time.Duration
	ReadinessStability time.Duration
	RequestTimeout     time.Duration
	StopTimeout        time.Duration
	Logger             *log.Logger
	Client             *http.Client
}

// Process represents one DSH process tree started by the desktop application.
type Process struct {
	cmd      *exec.Cmd
	done     chan struct{}
	mu       sync.Mutex
	waitErr  error
	stopOnce sync.Once
	stopErr  error
}

// Supervisor serialises ownership of the DSH process and provides a reusable
// readiness probe.
type Supervisor struct {
	config    Config
	client    *http.Client
	terminate func(int, bool) error
	mu        sync.Mutex
	active    *Process
	closed    bool
	stopMu    sync.Mutex
}

// NewSupervisor constructs a DSH supervisor with conservative defaults.
func NewSupervisor(config Config) *Supervisor {
	if config.ReadinessInterval <= 0 {
		config.ReadinessInterval = 250 * time.Millisecond
	}
	if config.ReadinessStability <= 0 {
		config.ReadinessStability = time.Second
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 2 * time.Second
	}
	if config.StopTimeout <= 0 {
		config.StopTimeout = 5 * time.Second
	}
	if config.Logger == nil {
		config.Logger = log.Default()
	}
	client := config.Client
	if client == nil {
		client = &http.Client{
			Timeout: config.RequestTimeout,
			Transport: &http.Transport{
				Proxy: nil,
			},
		}
	}
	return &Supervisor{config: config, client: client, terminate: terminateProcessTree}
}

// Start launches and adopts one DSH process. It refuses to overwrite a live
// process already owned by this supervisor.
func (supervisor *Supervisor) Start(
	ctx context.Context,
	bunxPath string,
	workspace string,
	environment []string,
	output io.Writer,
) (*Process, error) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.closed {
		return nil, ErrClosed
	}
	if supervisor.active != nil && !supervisor.active.exited() {
		return nil, errors.New("a managed DSH process is already running")
	}

	command := newBunxCommand(ctx, bunxPath, supervisor.config.Package, "web", "--no-open")
	command.Dir = workspace
	command.Env = environment
	command.Stdout = output
	command.Stderr = output
	configureChildProcess(command)
	if err := command.Start(); err != nil {
		return nil, err
	}
	process := &Process{cmd: command, done: make(chan struct{})}
	supervisor.active = process
	go process.wait()
	return process, nil
}

func (process *Process) wait() {
	err := process.cmd.Wait()
	process.mu.Lock()
	process.waitErr = err
	process.mu.Unlock()
	close(process.done)
}

// Done closes when the launcher process exits.
func (process *Process) Done() <-chan struct{} {
	return process.done
}

// WaitError reports the launcher process result after Done closes.
func (process *Process) WaitError() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.waitErr
}

func (process *Process) exited() bool {
	select {
	case <-process.done:
		return true
	default:
		return false
	}
}

// HasManagedProcess reports whether the supervisor owns a live DSH launcher.
func (supervisor *Supervisor) HasManagedProcess() bool {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.active != nil && !supervisor.active.exited()
}

// ClearIfCurrent releases ownership only when process is still current.
func (supervisor *Supervisor) ClearIfCurrent(process *Process) bool {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.active != process {
		return false
	}
	supervisor.active = nil
	return true
}

// StopIfCurrent releases and stops process when it is still current.
func (supervisor *Supervisor) StopIfCurrent(process *Process) error {
	if !supervisor.ClearIfCurrent(process) {
		return nil
	}
	return supervisor.stop(process)
}

// StopCurrent releases and stops the currently owned process.
func (supervisor *Supervisor) StopCurrent() error {
	supervisor.mu.Lock()
	process := supervisor.active
	supervisor.active = nil
	supervisor.mu.Unlock()
	if process == nil {
		return nil
	}
	return supervisor.stop(process)
}

// Close prevents future launches and stops any process currently owned by the
// supervisor. Start and Close are serialised so shutdown cannot orphan a late
// child process.
func (supervisor *Supervisor) Close() error {
	supervisor.mu.Lock()
	supervisor.closed = true
	process := supervisor.active
	supervisor.active = nil
	supervisor.mu.Unlock()
	supervisor.stopMu.Lock()
	defer supervisor.stopMu.Unlock()
	var err error
	if process != nil {
		err = process.stop(supervisor.config.StopTimeout, supervisor.config.Logger, supervisor.terminate)
	}
	supervisor.client.CloseIdleConnections()
	return err
}

func (supervisor *Supervisor) stop(process *Process) error {
	supervisor.stopMu.Lock()
	defer supervisor.stopMu.Unlock()
	return process.stop(supervisor.config.StopTimeout, supervisor.config.Logger, supervisor.terminate)
}

func (process *Process) stop(timeout time.Duration, logger *log.Logger, terminate func(int, bool) error) error {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return nil
	}
	process.stopOnce.Do(func() {
		if process.exited() {
			return
		}
		processID := process.cmd.Process.Pid
		logger.Printf("stopping DSH process tree (pid %d)", processID)
		var failures []error
		if err := terminate(processID, false); err != nil {
			logger.Printf("graceful DSH process-tree termination failed: %v", err)
			failures = append(failures, fmt.Errorf("terminate DSH process tree: %w", err))
		}
		if waitForExit(process.done, timeout) {
			return
		}

		logger.Printf("forcing DSH process tree to stop")
		if err := terminate(processID, true); err != nil {
			logger.Printf("forced DSH process-tree termination failed: %v", err)
			failures = append(failures, fmt.Errorf("force DSH process tree to stop: %w", err))
		}
		if waitForExit(process.done, timeout) {
			return
		}
		failures = append(failures, errors.New("DSH process did not exit after forced termination"))
		process.stopErr = errors.Join(failures...)
	})
	return process.stopErr
}

func waitForExit(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// Probe identifies whether the configured address is unavailable, is DSH, or
// belongs to an unexpected HTTP service.
func (supervisor *Supervisor) Probe(ctx context.Context) ProbeStatus {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, supervisor.config.URL, nil)
	if err != nil {
		return ProbeUnexpected
	}
	response, err := supervisor.client.Do(request)
	if err != nil {
		return ProbeUnavailable
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, responseBodyLimit))
	if readErr != nil {
		return ProbeUnexpected
	}
	matchesDSH := strings.Contains(strings.ToLower(string(body)), strings.ToLower(supervisor.config.PageMarker))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
		if matchesDSH {
			return ProbeUnavailable
		}
		return ProbeUnexpected
	}
	if !matchesDSH {
		return ProbeUnexpected
	}
	return ProbeReady
}

// WaitForReady requires the DSH identity check to remain successful for the
// configured stability interval.
func (supervisor *Supervisor) WaitForReady(ctx context.Context, process *Process, timeout time.Duration) error {
	return supervisor.waitForReady(ctx, process, timeout, supervisor.config.ReadinessInterval, supervisor.config.ReadinessStability)
}

func (supervisor *Supervisor) waitForReady(
	ctx context.Context,
	process *Process,
	timeout time.Duration,
	interval time.Duration,
	stability time.Duration,
) error {
	var processDone <-chan struct{}
	if process != nil {
		processDone = process.Done()
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var readySince time.Time

	for {
		switch supervisor.Probe(ctx) {
		case ProbeReady:
			if readySince.IsZero() {
				readySince = time.Now()
			}
			if time.Since(readySince) >= stability {
				return nil
			}
		case ProbeUnexpected:
			return fmt.Errorf("%s is occupied by a service that is not DSH", supervisor.config.URL)
		default:
			readySince = time.Time{}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-processDone:
			if err := process.WaitError(); err != nil {
				return fmt.Errorf("DSH 在服务就绪前退出：%w", err)
			}
			return errors.New("DSH 在服务就绪前退出")
		case <-deadline.C:
			return fmt.Errorf("等待 DSH 启动超时（%s）", timeout)
		case <-ticker.C:
		}
	}
}
