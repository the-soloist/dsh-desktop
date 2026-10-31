package npmregistry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func registryHTTPClient(handler roundTripFunc) *http.Client {
	return &http.Client{Transport: handler}
}

func registryResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestLatestVersionResolvesScopedPackage(t *testing.T) {
	var requestPath string
	httpClient := registryHTTPClient(func(request *http.Request) (*http.Response, error) {
		requestPath = request.URL.EscapedPath()
		return registryResponse(http.StatusOK, `{
			"dist-tags": {"latest":"0.1.5-rc.2","next":"0.1.5-rc.3","alpha":"0.1.7-alpha.2"},
			"versions": {"0.1.5-rc.2":{},"0.1.5-rc.3":{},"0.1.7-alpha.2":{},"latest":{}}
		}`), nil
	})
	client, err := NewClient("https://registry.example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.LatestVersion(context.Background(), "@deepseek-ai/dsh")
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if version != "0.1.5-rc.3" {
		t.Fatalf("LatestVersion() = %q", version)
	}
	if requestPath != "/@deepseek-ai%2Fdsh" {
		t.Fatalf("registry request path = %q", requestPath)
	}
}

func TestLatestVersionUsesSemverPrecedence(t *testing.T) {
	httpClient := registryHTTPClient(func(*http.Request) (*http.Response, error) {
		return registryResponse(http.StatusOK, `{
			"dist-tags": {"latest":"0.1.9"},
			"versions": {"0.1.9":{},"0.1.10":{},"1.0.0-alpha.2":{},"1.0.0":{}}
		}`), nil
	})
	client, err := NewClient("https://registry.example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.LatestVersion(context.Background(), "example")
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if version != "1.0.0" {
		t.Fatalf("LatestVersion() = %q", version)
	}
}

func TestLatestVersionSkipsAlphaVersions(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		want     string
	}{
		{
			name: "alpha tags cannot override published rc",
			metadata: `{
				"dist-tags": {"latest":"2.0.0-alpha.1","next":"3.0.0-alpha"},
				"versions": {"1.0.0-rc.2":{},"1.0.0-rc.10":{},"2.0.0-alpha.1":{}}
			}`,
			want: "1.0.0-rc.10",
		},
		{
			name: "beta remains eligible",
			metadata: `{
				"versions": {"1.0.0-rc.1":{},"2.0.0-beta.1":{},"3.0.0-alpha.1":{}}
			}`,
			want: "2.0.0-beta.1",
		},
		{
			name: "stable release remains eligible",
			metadata: `{
				"versions": {"1.0.0-rc.1":{},"1.0.0":{},"2.0.0-alpha.1":{}}
			}`,
			want: "1.0.0",
		},
		{
			name: "alpha build metadata does not change its channel",
			metadata: `{
				"versions": {"1.0.0":{},"2.0.0-alpha.1+build.2":{},"3.0.0-ALPHA.1":{}}
			}`,
			want: "1.0.0",
		},
		{
			name: "alpha text in build metadata is not an alpha prerelease",
			metadata: `{
				"versions": {"1.0.0":{},"2.0.0+build-alpha.1":{},"3.0.0-alpha.1":{}}
			}`,
			want: "2.0.0+build-alpha.1",
		},
		{
			name: "non-alpha tag remains eligible",
			metadata: `{
				"dist-tags": {"next":"2.0.0-rc.1","alpha":"3.0.0-alpha.1"},
				"versions": {"1.0.0":{}}
			}`,
			want: "2.0.0-rc.1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			httpClient := registryHTTPClient(func(*http.Request) (*http.Response, error) {
				return registryResponse(http.StatusOK, test.metadata), nil
			})
			client, err := NewClient("https://registry.example.test", httpClient)
			if err != nil {
				t.Fatal(err)
			}
			version, err := client.LatestVersion(context.Background(), "example")
			if err != nil {
				t.Fatalf("LatestVersion() error = %v", err)
			}
			if version != test.want {
				t.Fatalf("LatestVersion() = %q, want %q", version, test.want)
			}
		})
	}
}

func TestLatestVersionRejectsAlphaOnlyRegistryResponse(t *testing.T) {
	httpClient := registryHTTPClient(func(*http.Request) (*http.Response, error) {
		return registryResponse(http.StatusOK, `{
			"dist-tags": {"latest":"1.0.0-alpha.1","alpha":"2.0.0-alpha.1"},
			"versions": {"1.0.0-alpha":{},"1.0.0-alpha.1":{}}
		}`), nil
	})
	client, err := NewClient("https://registry.example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.LatestVersion(context.Background(), "example")
	if version != "" || err == nil || !strings.Contains(err.Error(), "excluding alpha") {
		t.Fatalf("LatestVersion() = (%q, %v), want no version and an alpha exclusion error", version, err)
	}
}

func TestLatestVersionRejectsInvalidRegistryResponse(t *testing.T) {
	httpClient := registryHTTPClient(func(*http.Request) (*http.Response, error) {
		return registryResponse(http.StatusOK, `{"version":"latest"}`), nil
	})
	client, err := NewClient("https://registry.example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.LatestVersion(context.Background(), "example")
	if err == nil || !strings.Contains(err.Error(), "no valid versions") {
		t.Fatalf("LatestVersion() error = %v", err)
	}
}

func TestLatestVersionReportsRegistryFailure(t *testing.T) {
	httpClient := registryHTTPClient(func(*http.Request) (*http.Response, error) {
		return registryResponse(http.StatusNotFound, "not found"), nil
	})
	client, err := NewClient("https://registry.example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.LatestVersion(context.Background(), "example")
	if err == nil || !strings.Contains(err.Error(), "404 Not Found") {
		t.Fatalf("LatestVersion() error = %v", err)
	}
}
