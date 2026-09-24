package dshenv

import "testing"

func TestExpandShortPathUsesLongParent(t *testing.T) {
	resolve := func(path string) (string, bool) {
		if path == `C:\Users\ADMIN~1` {
			return `C:\Users\Administrator`, true
		}
		return "", false
	}
	got := expandShortPath(`C:\Users\ADMIN~1\AppData\Local\Temp`, resolve)
	const want = `C:\Users\Administrator\AppData\Local\Temp`
	if got != want {
		t.Fatalf("expandShortPath() = %q, want %q", got, want)
	}
}

func TestExpandShortPathLeavesOrdinaryPath(t *testing.T) {
	got := expandShortPath(`C:\Users\Administrator`, func(string) (string, bool) {
		t.Fatal("ordinary path was resolved")
		return "", false
	})
	if got != `C:\Users\Administrator` {
		t.Fatalf("expandShortPath() = %q", got)
	}
}

func TestExpandEnvironmentShortPathsRewritesSettingsPaths(t *testing.T) {
	resolve := func(path string) (string, bool) {
		switch path {
		case `C:\Users\ADMIN~1`:
			return `C:\Users\Administrator`, true
		case `C:\Users\ADMIN~1\bin`:
			return `C:\Users\Administrator\bin`, true
		default:
			return "", false
		}
	}
	got := expandEnvironmentShortPaths([]string{
		`USERPROFILE=C:\Users\ADMIN~1`,
		`PATH=C:\Windows;C:\Users\ADMIN~1\bin`,
		`OTHER=keep~1`,
	}, resolve)
	if EnvironmentValue(got, "USERPROFILE") != `C:\Users\Administrator` {
		t.Fatalf("USERPROFILE = %q", EnvironmentValue(got, "USERPROFILE"))
	}
	if EnvironmentValue(got, "PATH") != `C:\Windows;C:\Users\Administrator\bin` {
		t.Fatalf("PATH = %q", EnvironmentValue(got, "PATH"))
	}
	if EnvironmentValue(got, "OTHER") != "keep~1" {
		t.Fatalf("OTHER = %q", EnvironmentValue(got, "OTHER"))
	}
}
