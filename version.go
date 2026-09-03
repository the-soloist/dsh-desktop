// Package dshdesktop exposes build metadata shared by the application and
// platform packaging tools.
package dshdesktop

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

// embeddedVersion is the repository VERSION file captured at compile time.
//
//go:embed VERSION
var embeddedVersion string

// SemVersion is the numeric semantic version used by native package formats.
type SemVersion struct {
	Major uint16
	Minor uint16
	Patch uint16
}

// CurrentVersion parses the VERSION file embedded in this Go package.
func CurrentVersion() (SemVersion, error) {
	return ParseVersion(embeddedVersion)
}

// ParseVersion parses a strict major.minor.patch version whose components fit
// the uint16 fields used by Windows version resources.
func ParseVersion(value string) (SemVersion, error) {
	normalised := strings.TrimSpace(value)
	parts := strings.Split(normalised, ".")
	if len(parts) != 3 {
		return SemVersion{}, fmt.Errorf("invalid semantic version %q: expected major.minor.patch", normalised)
	}

	parsed := [3]uint16{}
	for index, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return SemVersion{}, fmt.Errorf("invalid semantic version component %q", part)
		}
		number, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return SemVersion{}, fmt.Errorf("invalid semantic version component %q: %w", part, err)
		}
		parsed[index] = uint16(number)
	}

	return SemVersion{Major: parsed[0], Minor: parsed[1], Patch: parsed[2]}, nil
}

func (version SemVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}
