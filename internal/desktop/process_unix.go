//go:build !windows

package desktop

func ownsStartupConsole() bool {
	return false
}

func hideStartupConsole() {}
