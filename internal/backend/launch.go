package backend

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/the-soloist/dsh-desktop/internal/profile"
)

// Launch describes one invocation. Args and DisplayCommand deliberately share
// a single argument list so diagnostics cannot claim a different profile.
type Launch struct {
	RunnerPath       string
	PackageReference string
	Workspace        string
	Environment      []string
	Profile          string
	Port             int
}

func (launch Launch) Args() ([]string, error) {
	if err := profile.ValidateName(launch.Profile); err != nil {
		return nil, err
	}
	if launch.Port < 1 || launch.Port > 65535 {
		return nil, fmt.Errorf("invalid DSH port %d", launch.Port)
	}
	return []string{launch.PackageReference, "--profile", launch.Profile, "--no-open", "--port", strconv.Itoa(launch.Port)}, nil
}

func (launch Launch) DisplayCommand(runnerName string) (string, error) {
	arguments, err := launch.Args()
	if err != nil {
		return "", err
	}
	for index, argument := range arguments {
		if strings.ContainsAny(argument, " \t\r\n\"'\\") {
			arguments[index] = strconv.Quote(argument)
		}
	}
	return runnerName + " " + strings.Join(arguments, " "), nil
}
