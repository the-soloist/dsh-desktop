package dshdesktop

import (
	"strings"
	"testing"
)

func TestCurrentVersion(t *testing.T) {
	version, err := CurrentVersion()
	if err != nil {
		t.Fatalf("CurrentVersion() error = %v", err)
	}
	want := strings.TrimSpace(embeddedVersion)
	if version.String() != want {
		t.Fatalf("CurrentVersion() = %q, want embedded VERSION %q", version.String(), want)
	}
}

func TestParseVersionRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "1.2", "1.2.3.4", "01.2.3", "1.x.3", "65536.0.0"} {
		if _, err := ParseVersion(value); err == nil {
			t.Errorf("ParseVersion(%q) unexpectedly succeeded", value)
		}
	}
}
