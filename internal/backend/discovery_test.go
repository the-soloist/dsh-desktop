package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestIsDSHCommand(t *testing.T) {
	for _, command := range []string{
		"bunx @deepseek-ai/dsh@0.1.2 web --port 4567",
		"npx --yes @deepseek-ai/dsh web",
		"npm exec @deepseek-ai/dsh@0.1.2 web",
		"bun x @deepseek-ai/dsh web",
		"node /cache/node_modules/@deepseek-ai/dsh/dist/cli.js web --port 4567",
		"node /node/npm/bin/npx-cli.js @deepseek-ai/dsh web",
		"node /usr/local/bin/dsh web",
		"node /cache/node_modules/.bin/dsh web --port 9000",
		`"C:\Program Files\nodejs\node.exe" "C:\Users\A B\node_modules\@deepseek-ai\dsh\dist\cli.js" web`,
		"/usr/local/bin/dsh web --port 9000",
		"dsh",
	} {
		if !isDSHCommand(command) {
			t.Errorf("missed DSH command %q", command)
		}
	}
	for _, command := range []string{
		"", "dsh-desktop", "node server.js --port 3080",
		"grep @deepseek-ai/dsh", "vim /cache/node_modules/@deepseek-ai/dsh/cli.js",
		"sh -c bunx @deepseek-ai/dsh web", "npm install @deepseek-ai/dsh",
		"node -e 'console.log(\"@deepseek-ai/dsh\")'",
		"node server.js @deepseek-ai/dsh", "bunx @deepseek-ai/dsh-other",
		"bunx unrelated-tool @deepseek-ai/dsh", "npx other @deepseek-ai/dsh",
		"/usr/bin/python dsh.py",
	} {
		if isDSHCommand(command) {
			t.Errorf("misidentified unrelated process %q", command)
		}
	}
}

func TestFindDSHProcessesIncludesOtherPortsAndDeduplicatesChildren(t *testing.T) {
	processes := []processIdentity{
		{PID: 10, ParentPID: 1, Started: "a", Command: "bunx @deepseek-ai/dsh web --port 9000"},
		{PID: 11, ParentPID: 10, Started: "b", Command: "node /cache/@deepseek-ai/dsh/dist/cli.js web --port 9000"},
		{PID: 12, ParentPID: 11, Started: "c", Command: "python worker.py"},
		{PID: 20, ParentPID: 1, Started: "d", Command: "dsh web --port 9001"},
		{PID: 30, ParentPID: 1, Started: "e", Command: "node unrelated.js --port 3080"},
	}
	got := findDSHProcesses(processes, 99)
	var pids []int
	for _, process := range got {
		pids = append(pids, process.PID)
	}
	if !reflect.DeepEqual(pids, []int{10, 20}) {
		t.Fatalf("DSH instances = %v", pids)
	}
}

func TestFindDSHProcessesExcludesOwnAppImageLauncherAndAncestors(t *testing.T) {
	processes := []processIdentity{
		// ps prints this executable path without quotes. The space makes the
		// command parser see "DSH" as argv[0]; it must never become a target
		// for our own process-discovery confirmation or termination.
		{PID: 10, ParentPID: 1, Started: "wrapper", Command: "/tmp/dist/DSH Desktop.AppImage"},
		{PID: 11, ParentPID: 10, Started: "wrapper-child", Command: "sh /tmp/appimage/AppRun"},
		{PID: 12, ParentPID: 11, Started: "self", Command: "/tmp/appimage/usr/bin/DSH Desktop"},
		{PID: 20, ParentPID: 1, Started: "external", Command: "bunx @deepseek-ai/dsh --profile work --port 3080"},
	}
	got := findDSHProcesses(processes, 12)
	if len(got) != 1 || got[0].PID != 20 {
		t.Fatalf("process discovery included its own launcher: %+v", got)
	}
}

func TestStopDSHProcessesRejectsReusedPID(t *testing.T) {
	old := processIdentity{PID: 10, Started: "old", Command: "dsh web"}
	confirmed := []DSHProcess{{PID: old.PID, identity: old}}
	snapshot := func(context.Context) ([]processIdentity, error) {
		return []processIdentity{{PID: 10, Started: "new", Command: "node unrelated.js"}}, nil
	}
	signal := func(context.Context, processIdentity, bool) error {
		t.Fatal("signalled a reused PID")
		return nil
	}
	if err := stopDSHProcesses(context.Background(), confirmed, snapshot, signal); err == nil {
		t.Fatal("accepted changed identity")
	}
}

func TestStopDSHProcessesIncludesDescendantsButNotOtherInstances(t *testing.T) {
	root := processIdentity{PID: 10, Started: "a", Command: "dsh web"}
	current := []processIdentity{root,
		{PID: 11, ParentPID: 10, Started: "b", Command: "python plugin.py"},
		{PID: 12, ParentPID: 11, Started: "c", Command: "python worker.py"},
		{PID: 20, ParentPID: 1, Started: "d", Command: "dsh web --port 9000"},
		{PID: 30, ParentPID: 1, Started: "e", Command: "node unrelated.js"},
	}
	signalled := map[int]bool{}
	snapshot := func(context.Context) ([]processIdentity, error) {
		var result []processIdentity
		for _, process := range current {
			if !signalled[process.PID] {
				result = append(result, process)
			}
		}
		return result, nil
	}
	signal := func(_ context.Context, process processIdentity, _ bool) error {
		signalled[process.PID] = true
		return nil
	}
	err := stopDSHProcesses(context.Background(), []DSHProcess{{PID: root.PID, identity: root}}, snapshot, signal)
	if err != nil || !reflect.DeepEqual(signalled, map[int]bool{10: true, 11: true, 12: true}) {
		t.Fatalf("signalled=%v, err=%v", signalled, err)
	}
}

func TestStopDSHProcessesHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := processIdentity{PID: 10, Started: "a", Command: "dsh web"}
	err := stopDSHProcesses(ctx, []DSHProcess{{PID: 10, identity: root}},
		func(context.Context) ([]processIdentity, error) { return []processIdentity{root}, nil },
		func(context.Context, processIdentity, bool) error {
			t.Fatal("signalled after cancellation")
			return nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
