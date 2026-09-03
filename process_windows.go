//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNewProcessGroup = 0x00000200

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	user32                    = windows.NewLazySystemDLL("user32.dll")
	getConsoleWindow          = kernel32.NewProc("GetConsoleWindow")
	getConsoleProcessList     = kernel32.NewProc("GetConsoleProcessList")
	showWindow                = user32.NewProc("ShowWindow")
	messageBox                = user32.NewProc("MessageBoxW")
	startupConsoleWindow      uintptr
	startupConsoleWindowOwned bool
)

func newBunxCommand(ctx context.Context, bunxPath string, arguments ...string) *exec.Cmd {
	extension := strings.ToLower(filepath.Ext(bunxPath))
	if extension != ".cmd" && extension != ".bat" {
		return exec.CommandContext(ctx, bunxPath, arguments...)
	}
	parts := make([]string, 0, len(arguments)+1)
	parts = append(parts, quoteCommandPromptArgument(bunxPath))
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
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func terminateProcessTree(processID int, force bool) error {
	arguments := []string{"/PID", strconv.Itoa(processID), "/T"}
	if force {
		arguments = append(arguments, "/F")
	}
	command := exec.Command("taskkill.exe", arguments...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if _, lookupErr := os.FindProcess(processID); lookupErr != nil {
			return nil
		}
		return fmt.Errorf("taskkill: %w", err)
	}
	return nil
}

func ownsStartupConsole() bool {
	window, _, _ := getConsoleWindow.Call()
	startupConsoleWindow = window
	if window == 0 {
		return false
	}
	processes := make([]uint32, 2)
	count, _, _ := getConsoleProcessList.Call(
		uintptr(unsafePointer(&processes[0])),
		uintptr(len(processes)),
	)
	startupConsoleWindowOwned = count == 1
	return startupConsoleWindowOwned
}

func hideStartupConsole() {
	if startupConsoleWindowOwned && startupConsoleWindow != 0 {
		const swHide = 0
		showWindow.Call(startupConsoleWindow, swHide)
	}
}

func showPlatformWarning(title, message string) {
	titlePointer, titleErr := windows.UTF16PtrFromString(title)
	messagePointer, messageErr := windows.UTF16PtrFromString(message)
	if titleErr != nil || messageErr != nil {
		return
	}
	const mbIconWarning = 0x00000030
	messageBox.Call(
		0,
		uintptr(unsafePointer(messagePointer)),
		uintptr(unsafePointer(titlePointer)),
		mbIconWarning,
	)
}

func unsafePointer[T any](value *T) unsafe.Pointer {
	return unsafe.Pointer(value)
}
