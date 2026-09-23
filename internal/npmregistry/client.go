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

	"github.com/the-soloist/dsh-desktop/internal/update"
)

const (
	DefaultURL        = "https://registry.npmjs.org"
	responseBodyLimit = 4 * 1024 * 1024
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

// LatestVersion returns the highest published semantic version of packageName.
// Prereleases count. The npm latest dist-tag is ignored because publishers can
// leave it on an older channel while a newer alpha or next version exists.
func (client *Client) LatestVersion(ctx context.Context, packageName string) (string, error) {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" || strings.ContainsAny(packageName, " \t\r\n") {
		return "", errors.New("invalid empty or whitespace-containing npm package name")
	}
	endpoint := client.baseURL + "/" + url.PathEscape(packageName)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create npm registry request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.npm.install-v1+json, application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("query %s versions: %w", packageName, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseBodyLimit))
		return "", fmt.Errorf("query %s versions: npm registry returned %s", packageName, response.Status)
	}
	var document struct {
		DistTags map[string]string          `json:"dist-tags"`
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, responseBodyLimit)).Decode(&document); err != nil {
		return "", fmt.Errorf("decode %s versions: %w", packageName, err)
	}
	version, ok := highestVersion(document.Versions, document.DistTags)
	if !ok {
		return "", fmt.Errorf("npm registry returned no valid versions for %s", packageName)
	}
	return version, nil
}

func highestVersion(versions map[string]json.RawMessage, distTags map[string]string) (string, bool) {
	var (
		best       string
		bestParsed update.Version
		found      bool
	)
	consider := func(version string) {
		version = strings.TrimSpace(version)
		if !semanticVersion.MatchString(version) {
			return
		}
		parsed, err := update.ParseVersion(version)
		if err != nil {
			return
		}
		if !found {
			best = version
			bestParsed = parsed
			found = true
			return
		}
		switch update.CompareVersion(parsed, bestParsed) {
		case 1:
			best = version
			bestParsed = parsed
		case 0:
			if version > best {
				best = version
			}
		}
	}
	for version := range versions {
		consider(version)
	}
	for _, version := range distTags {
		consider(version)
	}
	return best, found
}
