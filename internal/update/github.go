// Package update checks GitHub Releases for a newer desktop build.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultRepository is the public repository that publishes desktop builds.
	DefaultRepository = "the-soloist/dsh-desktop"
	defaultAPIURL     = "https://api.github.com"
	responseBodyLimit = 4 * 1024 * 1024
	requestTimeout    = 15 * time.Second
)

// Release describes the compatible artifact in the newest GitHub release.
type Release struct {
	Version     string
	TagName     string
	Name        string
	Notes       string
	HTMLURL     string
	DownloadURL string
	AssetName   string
	PublishedAt time.Time
}

// Client checks one GitHub repository for a compatible desktop release.
type Client struct {
	repository string
	baseURL    string
	http       *http.Client
	platform   string
	arch       string
}

// NewClient creates a GitHub Releases client. An empty repository uses
// DefaultRepository; an empty API URL uses api.github.com.
func NewClient(repository, apiURL string, httpClient *http.Client) (*Client, error) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		repository = DefaultRepository
	}
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.ContainsAny(repository, " \t\r\n") {
		return nil, fmt.Errorf("invalid GitHub repository %q; expected owner/repo", repository)
	}

	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid GitHub API URL %q", apiURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{
		repository: repository,
		baseURL:    apiURL,
		http:       httpClient,
		platform:   runtime.GOOS,
		arch:       runtime.GOARCH,
	}, nil
}

// WithPlatform returns a copy that checks assets for the supplied platform
// and architecture. It is primarily useful for tests and build validation.
func (client *Client) WithPlatform(platform, arch string) *Client {
	copy := *client
	copy.platform = platform
	copy.arch = arch
	return &copy
}

// Check returns the newest published release newer than currentVersion that
// contains the exact DSH-Desktop artifact for the current platform. It
// returns (nil, nil) when no compatible update is available.
func (client *Client) Check(ctx context.Context, currentVersion string) (*Release, error) {
	current, err := ParseVersion(currentVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid current application version: %w", err)
	}
	parts := strings.SplitN(client.repository, "/", 2)
	owner, repository := parts[0], parts[1]
	endpoint := client.baseURL + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/releases/latest"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create GitHub release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "DSH-Desktop")
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("query GitHub release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseBodyLimit))
		return nil, fmt.Errorf("query GitHub release: returned %s", response.Status)
	}

	var payload apiRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, responseBodyLimit)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode GitHub release: %w", err)
	}
	if payload.Draft || payload.Prerelease || strings.TrimSpace(payload.TagName) == "" {
		return nil, nil
	}
	releaseVersion, err := ParseVersion(payload.TagName)
	if err != nil {
		return nil, fmt.Errorf("GitHub release has invalid tag %q: %w", payload.TagName, err)
	}
	if CompareVersion(releaseVersion, current) <= 0 {
		return nil, nil
	}

	wantedAsset := AssetName(client.platform, client.arch)
	var asset *apiAsset
	for index := range payload.Assets {
		if strings.EqualFold(strings.TrimSpace(payload.Assets[index].Name), wantedAsset) {
			asset = &payload.Assets[index]
			break
		}
	}
	if asset == nil || strings.TrimSpace(asset.BrowserDownloadURL) == "" {
		// A newer release without an artifact for this platform is not useful
		// to this process, so let the caller continue normally.
		return nil, nil
	}
	htmlURL := strings.TrimSpace(payload.HTMLURL)
	if htmlURL == "" {
		htmlURL = "https://github.com/" + client.repository + "/releases/tag/" + url.PathEscape(payload.TagName)
	}
	return &Release{
		Version:     releaseVersion.String(),
		TagName:     strings.TrimSpace(payload.TagName),
		Name:        strings.TrimSpace(payload.Name),
		Notes:       strings.TrimSpace(payload.Body),
		HTMLURL:     htmlURL,
		DownloadURL: strings.TrimSpace(asset.BrowserDownloadURL),
		AssetName:   strings.TrimSpace(asset.Name),
		PublishedAt: payload.PublishedAt,
	}, nil
}

type apiRelease struct {
	TagName     string     `json:"tag_name"`
	Name        string     `json:"name"`
	Body        string     `json:"body"`
	HTMLURL     string     `json:"html_url"`
	PublishedAt time.Time  `json:"published_at"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	Assets      []apiAsset `json:"assets"`
}

type apiAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// AssetName returns the release artifact name used by the build workflow.
func AssetName(platform, arch string) string {
	return fmt.Sprintf("DSH-Desktop-%s-%s.7z", releasePlatform(platform), releaseArch(arch))
}

func releasePlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "darwin", "mac", "macos", "osx":
		return "macos"
	case "windows", "win":
		return "windows"
	case "linux":
		return "linux"
	default:
		return strings.ToLower(strings.TrimSpace(platform))
	}
}

func releaseArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64", "x86-64", "x64", "x86_64":
		return "x86_64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(arch))
	}
}

// Version is the semver subset accepted for application release tags.
type Version struct {
	Major      uint64
	Minor      uint64
	Patch      uint64
	PreRelease []identifier
}

type identifier struct {
	text    string
	numeric bool
	number  uint64
}

// ParseVersion accepts numeric major.minor.patch versions with optional
// prerelease/build metadata and an optional v prefix.
func ParseVersion(value string) (Version, error) {
	normalised := strings.TrimSpace(value)
	normalised = strings.TrimPrefix(strings.TrimPrefix(normalised, "v"), "V")
	if normalised == "" {
		return Version{}, errors.New("version is empty")
	}
	withoutBuild := strings.SplitN(normalised, "+", 2)[0]
	coreAndPre := strings.SplitN(withoutBuild, "-", 2)
	core := strings.Split(coreAndPre[0], ".")
	if len(core) != 3 {
		return Version{}, fmt.Errorf("expected major.minor.patch, got %q", value)
	}
	parsed := Version{}
	components := []*uint64{&parsed.Major, &parsed.Minor, &parsed.Patch}
	for index, part := range core {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return Version{}, fmt.Errorf("invalid numeric version component %q", part)
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("invalid numeric version component %q: %w", part, err)
		}
		*components[index] = number
	}
	if len(coreAndPre) == 1 {
		return parsed, nil
	}
	for _, part := range strings.Split(coreAndPre[1], ".") {
		if part == "" {
			return Version{}, fmt.Errorf("invalid prerelease component in %q", value)
		}
		for _, character := range part {
			if !(character == '-' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
				return Version{}, fmt.Errorf("invalid prerelease component %q", part)
			}
		}
		item := identifier{text: part}
		if number, err := strconv.ParseUint(part, 10, 64); err == nil {
			if len(part) > 1 && part[0] == '0' {
				return Version{}, fmt.Errorf("invalid numeric prerelease component %q", part)
			}
			item.numeric = true
			item.number = number
		}
		parsed.PreRelease = append(parsed.PreRelease, item)
	}
	return parsed, nil
}

func (version Version) String() string {
	result := fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
	if len(version.PreRelease) > 0 {
		parts := make([]string, len(version.PreRelease))
		for index, item := range version.PreRelease {
			parts[index] = item.text
		}
		result += "-" + strings.Join(parts, ".")
	}
	return result
}

// CompareVersion compares two semver values using SemVer 2.0 precedence.
func CompareVersion(first, second Version) int {
	for _, pair := range [][2]uint64{{first.Major, second.Major}, {first.Minor, second.Minor}, {first.Patch, second.Patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(first.PreRelease) == 0 && len(second.PreRelease) == 0 {
		return 0
	}
	if len(first.PreRelease) == 0 {
		return 1
	}
	if len(second.PreRelease) == 0 {
		return -1
	}
	for index := 0; index < len(first.PreRelease) && index < len(second.PreRelease); index++ {
		a, b := first.PreRelease[index], second.PreRelease[index]
		if a.numeric && b.numeric {
			if a.number < b.number {
				return -1
			}
			if a.number > b.number {
				return 1
			}
			continue
		}
		if a.numeric != b.numeric {
			if a.numeric {
				return -1
			}
			return 1
		}
		if a.text < b.text {
			return -1
		}
		if a.text > b.text {
			return 1
		}
	}
	if len(first.PreRelease) < len(second.PreRelease) {
		return -1
	}
	if len(first.PreRelease) > len(second.PreRelease) {
		return 1
	}
	return 0
}
