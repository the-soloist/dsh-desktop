//go:build !windows

package dshenv

func expandLaunchEnvironmentPaths(environment []string) []string {
	return environment
}

func expandLaunchPath(path string) string {
	return path
}
