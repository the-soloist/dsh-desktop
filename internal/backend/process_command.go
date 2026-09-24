package backend

import (
	"path"
	"regexp"
	"strings"
)

var commandArgumentPattern = regexp.MustCompile(`"[^"]*"|'[^']*'|[^\s]+`)

func isDSHCommand(command string) bool {
	arguments := commandArgumentPattern.FindAllString(command, -1)
	for index := range arguments {
		arguments[index] = strings.Trim(arguments[index], `"'`)
	}
	if len(arguments) == 0 {
		return false
	}
	name := executableBase(arguments[0])
	if name == "dsh" {
		return true
	}
	arguments = arguments[1:]
	switch name {
	case "node", "bun":
		if name == "bun" && len(arguments) > 0 && arguments[0] == "x" {
			return hasDSHPackageArgument(arguments[1:])
		}
		for index, argument := range arguments {
			if argument == "-e" || argument == "--eval" || argument == "-p" || argument == "--print" || strings.HasPrefix(argument, "--eval=") {
				return false
			}
			if strings.HasPrefix(argument, "-") || name == "bun" && argument == "run" {
				continue
			}
			script := strings.ReplaceAll(argument, `\`, "/")
			if strings.Contains(script, "/@deepseek-ai/dsh/") || strings.Contains(script, "/@deepseek-ai/dsh@") || executableBase(script) == "dsh" {
				return true
			}
			if path.Base(script) == "npx-cli.js" {
				return hasDSHPackageArgument(arguments[index+1:])
			}
			return false
		}
	case "bunx", "npx":
		return hasDSHPackageArgument(arguments)
	case "npm":
		if len(arguments) > 0 && (arguments[0] == "exec" || arguments[0] == "x") {
			return hasDSHPackageArgument(arguments[1:])
		}
	}
	return false
}

func executableBase(value string) string {
	name := strings.ToLower(path.Base(strings.ReplaceAll(value, `\`, "/")))
	for _, suffix := range []string{".exe", ".cmd", ".bat"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return name
}

func hasDSHPackageArgument(arguments []string) bool {
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "-") {
			continue
		}
		return argument == "@deepseek-ai/dsh" || strings.HasPrefix(argument, "@deepseek-ai/dsh@")
	}
	return false
}
