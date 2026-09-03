package dshdesktop

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Metadata contains application identity and DSH runtime settings shared by
// the desktop application and native packaging tools.
type Metadata struct {
	DisplayName      string `json:"displayName"`
	InternalName     string `json:"internalName"`
	Description      string `json:"description"`
	BundleIdentifier string `json:"bundleIdentifier"`
	LinuxDesktopID   string `json:"linuxDesktopId"`
	DSHPackage       string `json:"dshPackage"`
	DSHURL           string `json:"dshURL"`
	DSHPageMarker    string `json:"dshPageMarker"`
}

//go:embed APP_METADATA.json
var embeddedMetadata []byte

var (
	metadataOnce sync.Once
	metadata     Metadata
	metadataErr  error
)

// CurrentMetadata parses and validates the embedded APP_METADATA.json file.
func CurrentMetadata() (Metadata, error) {
	metadataOnce.Do(func() {
		metadataErr = json.Unmarshal(embeddedMetadata, &metadata)
		if metadataErr == nil {
			metadataErr = validateMetadata(metadata)
		}
	})
	return metadata, metadataErr
}

func validateMetadata(value Metadata) error {
	required := map[string]string{
		"displayName":      value.DisplayName,
		"internalName":     value.InternalName,
		"description":      value.Description,
		"bundleIdentifier": value.BundleIdentifier,
		"linuxDesktopId":   value.LinuxDesktopID,
		"dshPackage":       value.DSHPackage,
		"dshURL":           value.DSHURL,
		"dshPageMarker":    value.DSHPageMarker,
	}
	for name, field := range required {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("application metadata field %s is empty", name)
		}
	}
	parsedURL, err := url.ParseRequestURI(value.DSHURL)
	if err != nil || parsedURL.Scheme != "http" || parsedURL.Hostname() != "127.0.0.1" {
		return fmt.Errorf("application metadata dshURL must be an HTTP loopback URL: %q", value.DSHURL)
	}
	return nil
}
