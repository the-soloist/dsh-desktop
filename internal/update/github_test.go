package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetNameNormalisesRuntimeValues(t *testing.T) {
	tests := []struct {
		platform string
		arch     string
		want     string
	}{
		{platform: "darwin", arch: "amd64", want: "DSH-Desktop-macos-x86_64.7z"},
		{platform: "macOS", arch: "aarch64", want: "DSH-Desktop-macos-arm64.7z"},
		{platform: "windows", arch: "x64", want: "DSH-Desktop-windows-x86_64.7z"},
		{platform: "linux", arch: "amd64", want: "DSH-Desktop-linux-x86_64.7z"},
	}
	for _, test := range tests {
		if got := AssetName(test.platform, test.arch); got != test.want {
			t.Errorf("AssetName(%q, %q) = %q, want %q", test.platform, test.arch, got, test.want)
		}
	}
}

func TestParseVersionAndCompareSemverPrecedence(t *testing.T) {
	base, err := ParseVersion("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if base.String() != "1.2.3" {
		t.Fatalf("String() = %q", base.String())
	}
	for _, test := range []struct {
		left  string
		right string
		want  int
	}{
		{"1.2.3-alpha", "1.2.3", -1},
		{"1.2.3-alpha.1", "1.2.3-alpha.beta", -1},
		{"1.2.3", "1.2.3+build.1", 0},
		{"1.2.4", "1.2.3", 1},
	} {
		left, leftErr := ParseVersion(test.left)
		right, rightErr := ParseVersion(test.right)
		if leftErr != nil || rightErr != nil {
			t.Fatalf("parse %q/%q: %v/%v", test.left, test.right, leftErr, rightErr)
		}
		if got := CompareVersion(left, right); got != test.want {
			t.Errorf("CompareVersion(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestClientCheckPicksCompatibleLatestRelease(t *testing.T) {
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/the-soloist/dsh-desktop/releases/latest" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Accept"); !strings.Contains(got, "application/vnd.github+json") {
			t.Errorf("Accept = %q", got)
		}
		fmt.Fprintf(response, `{
  "tag_name": "v0.3.0",
  "name": "DSH Desktop 0.3.0",
  "body": "Bug fixes",
  "html_url": "https://github.com/the-soloist/dsh-desktop/releases/tag/v0.3.0",
  "published_at": "2026-09-05T00:00:00Z",
  "assets": [
    {"name": "DSH-Desktop-linux-x86_64.7z", "browser_download_url": "%s/download"},
    {"name": "SHA256SUMS.txt", "browser_download_url": "%s/checksums"}
  ]
}`, endpoint, endpoint)
	}))
	defer server.Close()
	endpoint = server.URL

	client, err := NewClient("", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := client.WithPlatform("linux", "amd64").Check(context.Background(), "0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	if release == nil {
		t.Fatal("expected a compatible update")
	}
	if release.Version != "0.3.0" || release.AssetName != "DSH-Desktop-linux-x86_64.7z" || release.DownloadURL != server.URL+"/download" {
		t.Fatalf("release = %+v", release)
	}
}

func TestClientCheckReturnsNilWhenReleaseHasNoCompatibleAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"tag_name":"v0.3.0","assets":[{"name":"DSH-Desktop-windows-x86_64.7z","browser_download_url":"https://example.invalid/app"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(DefaultRepository, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := client.WithPlatform("linux", "amd64").Check(context.Background(), "0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	if release != nil {
		t.Fatalf("expected nil release, got %+v", release)
	}
}

func TestClientCheckDoesNotOfferOlderRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"tag_name":"v0.2.1","assets":[{"name":"DSH-Desktop-linux-x86_64.7z","browser_download_url":"https://example.invalid/app"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(DefaultRepository, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := client.WithPlatform("linux", "amd64").Check(context.Background(), "0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	if release != nil {
		t.Fatalf("expected nil release, got %+v", release)
	}
}
