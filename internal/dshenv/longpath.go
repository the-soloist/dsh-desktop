package dshenv

import (
	"regexp"
	"strings"
)

var shortPathComponent = regexp.MustCompile(`(?i)(?:^|[\\/])[^\\/:]{1,6}~[0-9]+(?:[\\/]|$)`)

func hasShortPathComponent(path string) bool {
	return shortPathComponent.MatchString(path)
}

func expandShortPath(path string, resolve func(string) (string, bool)) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || resolve == nil || !hasShortPathComponent(trimmed) {
		return path
	}
	if long, ok := resolve(trimmed); ok && long != "" {
		return long
	}
	current := strings.TrimRight(trimmed, `\/`)
	var remainder []string
	for !isWindowsPathRoot(current) {
		parent, base := splitWindowsPath(current)
		if parent == current || base == "" {
			break
		}
		remainder = append(remainder, base)
		if hasShortPathComponent(parent) {
			if long, ok := resolve(parent); ok && long != "" {
				expanded := long
				for index := len(remainder) - 1; index >= 0; index-- {
					expanded = joinWindowsPath(expanded, remainder[index])
				}
				return expanded
			}
		}
		current = parent
	}
	return path
}

func expandEnvironmentShortPaths(environment []string, resolve func(string) (string, bool)) []string {
	if resolve == nil {
		return environment
	}
	changed := false
	result := make([]string, len(environment))
	for index, item := range environment {
		name, value, found := strings.Cut(item, "=")
		if !found || !hasShortPathComponent(value) {
			result[index] = item
			continue
		}
		expanded := value
		if strings.EqualFold(name, "PATH") {
			parts := strings.Split(value, ";")
			for partIndex, part := range parts {
				parts[partIndex] = expandShortPath(strings.TrimSpace(part), resolve)
			}
			expanded = strings.Join(parts, ";")
		} else if looksLikeWindowsPath(value) {
			expanded = expandShortPath(value, resolve)
		}
		if expanded != value {
			changed = true
			result[index] = name + "=" + expanded
			continue
		}
		result[index] = item
	}
	if !changed {
		return environment
	}
	return result
}

func looksLikeWindowsPath(value string) bool {
	if strings.ContainsAny(value, `\/`) {
		return true
	}
	return len(value) >= 2 && value[1] == ':'
}

func isWindowsPathRoot(path string) bool {
	trimmed := strings.TrimRight(path, `\/`)
	if len(trimmed) == 2 && trimmed[1] == ':' {
		return true
	}
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`) {
		parts := strings.FieldsFunc(trimmed, func(character rune) bool {
			return character == '\\' || character == '/'
		})
		return len(parts) <= 2
	}
	return trimmed == ""
}

func splitWindowsPath(path string) (string, string) {
	trimmed := strings.TrimRight(path, `\/`)
	index := strings.LastIndexAny(trimmed, `\/`)
	if index < 0 {
		return trimmed, ""
	}
	parent := strings.TrimRight(trimmed[:index], `\/`)
	base := trimmed[index+1:]
	if len(parent) == 2 && parent[1] == ':' {
		parent += `\`
	}
	return parent, base
}

func joinWindowsPath(parent, child string) string {
	parent = strings.TrimRight(parent, `\/`)
	child = strings.Trim(child, `\/`)
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	if len(parent) == 2 && parent[1] == ':' {
		return parent + `\` + child
	}
	return parent + `\` + child
}
