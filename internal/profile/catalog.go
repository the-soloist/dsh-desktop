// Package profile discovers DSH profiles without executing their plugins.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const Default = "web"

type Entry struct {
	Name   string
	Reason string // Non-empty entries are visible, but cannot be launched.
}

func ValidateName(name string) error {
	// npx may be a .cmd shim on Windows. Reject quote/expansion characters
	// rather than allowing a profile name to change cmd.exe interpretation.
	if name == "" || name != strings.TrimSpace(name) || strings.ContainsAny(name, `/\"%!`) ||
		name == "." || name == ".." || name == "node_modules" || strings.EqualFold(name, "desktop") ||
		strings.HasPrefix(name, "-") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("无效的 Profile 名称：%q", name)
	}
	return nil
}

// Discover always includes web: DSH can initialise its shipped template on
// first use. Other profiles must already have a valid manifest.
func Discover(home string) ([]Entry, error) {
	entries := []Entry{Inspect(home, Default)}
	directories, err := os.ReadDir(filepath.Join(home, "profiles"))
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return entries, err
	}
	for _, directory := range directories {
		name := directory.Name()
		if name == Default || strings.HasPrefix(name, ".") || ValidateName(name) != nil {
			continue
		}
		info, err := os.Stat(filepath.Join(home, "profiles", name))
		if err != nil || !info.IsDir() {
			continue
		}
		entries = append(entries, Inspect(home, name))
	}
	slices.SortFunc(entries[1:], func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return entries, nil
}

func Inspect(home, name string) Entry {
	entry := Entry{Name: name}
	if err := ValidateName(name); err != nil {
		entry.Reason = err.Error()
		return entry
	}
	data, err := os.ReadFile(filepath.Join(home, "profiles", name, "package.json"))
	if errors.Is(err, os.ErrNotExist) && name == Default {
		return entry
	}
	if err != nil {
		entry.Reason = "无法读取 package.json"
		return entry
	}
	var manifest struct {
		DSH struct {
			Profile struct {
				Bundles []string `json:"bundles"`
			} `json:"profile"`
		} `json:"dsh"`
	}
	if json.Unmarshal(data, &manifest) != nil || len(manifest.DSH.Profile.Bundles) == 0 {
		entry.Reason = "无效的 Profile 配置"
		return entry
	}
	for _, bundle := range manifest.DSH.Profile.Bundles {
		if strings.TrimSpace(bundle) == "" {
			entry.Reason = "无效的 Profile 配置"
			return entry
		}
	}
	// Only reject a composition made entirely of known non-web bundles.
	// Custom bundles may supply Web transitively; startup readiness remains the
	// authoritative check. Never run --dump-config while populating a menu.
	for _, bundle := range manifest.DSH.Profile.Bundles {
		switch bundle {
		case "@deepseek-ai/dsh-base", "@deepseek-ai/dsh-headless", "@deepseek-ai/dsh-sdk-app", "@deepseek-ai/dsh-sdk-minimal", "@deepseek-ai/dsh-acp-app":
		default:
			return entry
		}
	}
	entry.Reason = "不包含 Web 界面"
	return entry
}
