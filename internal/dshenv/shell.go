package dshenv

// ShellImport describes environment loaded from shell startup files.
type ShellImport struct {
	Shell     string
	Sources   []string
	Variables []string
}

// LoadShellEnvironment loads the user's normal environment on macOS/Linux.
// The Windows implementation returns a copy of the system environment.
func LoadShellEnvironment(base []string) ([]string, ShellImport, error) {
	return loadShellEnvironment(base)
}
