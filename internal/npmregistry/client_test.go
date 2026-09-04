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
		return registryResponse(http.StatusOK, `{"name":"@deepseek-ai/dsh","version":"0.1.2-rc.1"}`), nil
	})
	client, err := NewClient("https://registry.example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.LatestVersion(context.Background(), "@deepseek-ai/dsh")
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if version != "0.1.2-rc.1" {
		t.Fatalf("LatestVersion() = %q", version)
	}
	if requestPath != "/@deepseek-ai%2Fdsh/latest" {
		t.Fatalf("registry request path = %q", requestPath)
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
	if err == nil || !strings.Contains(err.Error(), "invalid latest version") {
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
