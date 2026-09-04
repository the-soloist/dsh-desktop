// Package npmregistry resolves exact package versions from an npm registry.
package npmregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultURL        = "https://registry.npmjs.org"
	responseBodyLimit = 1024 * 1024
	requestTimeout    = 15 * time.Second
)

var semanticVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// Client reads package metadata from one npm-compatible registry.
type Client struct {
	baseURL string
	http    *http.Client
}

// ExactReference joins a package name and a previously resolved exact version.
func ExactReference(packageName, version string) string {
	return packageName + "@" + version
}

// NewClient validates registryURL and creates a client. An empty URL selects
// the public npm registry.
func NewClient(registryURL string, httpClient *http.Client) (*Client, error) {
	registryURL = strings.TrimSpace(registryURL)
	if registryURL == "" {
		registryURL = DefaultURL
	}
	parsed, err := url.Parse(registryURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid npm registry URL %q", registryURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("npm registry URL must not contain a query or fragment: %q", registryURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{baseURL: strings.TrimRight(parsed.String(), "/"), http: httpClient}, nil
}

// LatestVersion returns the exact semantic version assigned to the latest
// dist-tag for packageName.
func (client *Client) LatestVersion(ctx context.Context, packageName string) (string, error) {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" || strings.ContainsAny(packageName, " \t\r\n") {
		return "", errors.New("invalid empty or whitespace-containing npm package name")
	}
	endpoint := client.baseURL + "/" + url.PathEscape(packageName) + "/latest"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create npm registry request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("query latest %s version: %w", packageName, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseBodyLimit))
		return "", fmt.Errorf("query latest %s version: npm registry returned %s", packageName, response.Status)
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, responseBodyLimit)).Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode latest %s version: %w", packageName, err)
	}
	metadata.Version = strings.TrimSpace(metadata.Version)
	if !semanticVersion.MatchString(metadata.Version) {
		return "", fmt.Errorf("npm registry returned invalid latest version %q for %s", metadata.Version, packageName)
	}
	return metadata.Version, nil
}
