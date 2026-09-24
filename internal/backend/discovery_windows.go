//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func snapshotProcesses(ctx context.Context) ([]processIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	const script = `$ErrorActionPreference = 'Stop'; [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false); $items = @(Get-CimInstance Win32_Process | ForEach-Object { if ($_.CommandLine -and $_.CreationDate) { @{ pid = [int]$_.ProcessId; parentPID = [int]$_.ParentProcessId; started = $_.CreationDate.ToFileTimeUtc().ToString(); command = $_.CommandLine } } }); ConvertTo-Json -InputObject $items -Compress`
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	output, err := command.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("inspect processes: %w", err)
	}
	var processes []processIdentity
	if err := json.Unmarshal(output, &processes); err != nil {
		return nil, fmt.Errorf("decode process identities: %w", err)
	}
	return processes, nil
}

func signalExternalProcess(ctx context.Context, target processIdentity, _ bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Holding a handle and checking creation time prevents PID reuse between
	// confirmation and termination. Windows has no SIGTERM for detached processes.
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(target.PID))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return err
	}
	if exited.HighDateTime != 0 || exited.LowDateTime != 0 {
		return nil
	}
	started := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	expected, err := strconv.ParseUint(target.Started, 10, 64)
	// CIM dates have microsecond precision; FILETIME has 100 ns precision.
	if err != nil || started/10 != expected/10 {
		return nil
	}
	return windows.TerminateProcess(handle, 1)
}
