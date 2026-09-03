//go:build linux

package desktop

import "os/exec"

func showPlatformWarning(title, message string) {
	commands := [][]string{
		{"zenity", "--warning", "--title", title, "--text", message},
		{"kdialog", "--title", title, "--sorry", message},
		{"xmessage", "-center", "-title", title, message},
	}
	for _, candidate := range commands {
		path, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		command := exec.Command(path, candidate[1:]...)
		if command.Run() == nil {
			return
		}
	}
}
