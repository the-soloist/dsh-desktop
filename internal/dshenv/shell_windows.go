//go:build windows

package dshenv

func loadShellEnvironment(base []string) ([]string, ShellImport, error) {
	return append([]string(nil), base...), ShellImport{}, nil
}
