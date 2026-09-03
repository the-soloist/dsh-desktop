//go:build darwin

package desktop

import (
	"encoding/json"
	"os/exec"
)

func showPlatformWarning(title, message string) {
	encodedTitle, _ := json.Marshal(title)
	encodedMessage, _ := json.Marshal(message)
	script := "const app = Application.currentApplication();" +
		"app.includeStandardAdditions = true;" +
		"app.displayDialog(" + string(encodedMessage) + ", {" +
		"withTitle: " + string(encodedTitle) + "," +
		"buttons: ['OK'], defaultButton: 'OK', withIcon: 'caution'" +
		"});"
	_ = exec.Command("osascript", "-l", "JavaScript", "-e", script).Run()
}
