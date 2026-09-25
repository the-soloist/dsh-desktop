package backend

import (
	"reflect"
	"testing"
)

func TestLaunchArgumentsAndDiagnostics(t *testing.T) {
	for _, name := range []string{"web", "work", "work profile"} {
		launch := Launch{PackageReference: "@deepseek-ai/dsh@0.1.7-rc.2", Profile: name, Port: 3081}
		arguments, err := launch.Args()
		want := []string{launch.PackageReference, "--profile", name, "--no-open", "--port", "3081"}
		if err != nil || !reflect.DeepEqual(arguments, want) {
			t.Fatalf("Args = %v, %v", arguments, err)
		}
		label := name
		if name == "work profile" {
			label = `"work profile"`
		}
		for _, runner := range []string{"bunx", "npx"} {
			command, err := launch.DisplayCommand(runner)
			if want := runner + " " + launch.PackageReference + " --profile " + label + " --no-open --port 3081"; err != nil || command != want {
				t.Fatalf("DisplayCommand = %q, %v; want %q", command, err, want)
			}
		}
	}
}

func TestLaunchRejectsInvalidProfileAndPort(t *testing.T) {
	for _, launch := range []Launch{{Profile: "../other", Port: 3080}, {Profile: "desktop", Port: 3080}, {Profile: "web", Port: 0}, {Profile: "web", Port: 65536}} {
		if _, err := launch.Args(); err == nil {
			t.Fatalf("accepted invalid launch: %+v", launch)
		}
	}
}
