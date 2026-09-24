//go:build windows

package dshenv

import (
	"strings"

	"golang.org/x/sys/windows"
)

func expandLaunchEnvironmentPaths(environment []string) []string {
	return expandEnvironmentShortPaths(environment, longPathName)
}

func expandLaunchPath(path string) string {
	return expandShortPath(path, longPathName)
}

func longPathName(path string) (string, bool) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	buffer := make([]uint16, 260)
	for {
		size, callErr := windows.GetLongPathName(pointer, &buffer[0], uint32(len(buffer)))
		if size == 0 || callErr != nil && int(size) <= len(buffer) {
			return "", false
		}
		if int(size) <= len(buffer) {
			return trimExtendedPath(windows.UTF16ToString(buffer[:size])), true
		}
		buffer = make([]uint16, size)
	}
}

func trimExtendedPath(path string) string {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
	}
	return strings.TrimPrefix(path, `\\?\`)
}
