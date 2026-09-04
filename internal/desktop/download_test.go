package desktop

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingDownloadReader struct{}

func (failingDownloadReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestDownloadFilenameSanitisesPathSeparatorsAndReservedCharacters(t *testing.T) {
	got := downloadFilename(` report:2026/09/05?.zip `, "/api/session.export")
	if got != "report_2026_09_05_.zip" {
		t.Fatalf("downloadFilename() = %q", got)
	}
	if strings.ContainsAny(got, `/\\:*?<>|\"`) {
		t.Fatalf("downloadFilename() returned unsafe name %q", got)
	}
}

func TestDownloadFilenameFallsBackToRequestPath(t *testing.T) {
	if got := downloadFilename("", "/api/session.export"); got != "session.export" {
		t.Fatalf("downloadFilename() = %q, want session.export", got)
	}
	if got := downloadFilename("", "/"); got != "download" {
		t.Fatalf("downloadFilename() root fallback = %q, want download", got)
	}
}

func TestSameHTTPOriginIgnoresPathAndCase(t *testing.T) {
	if !sameHTTPOrigin("HTTP://127.0.0.1:1234/path", "http://127.0.0.1:1234/") {
		t.Fatal("sameHTTPOrigin() rejected equal HTTP origins")
	}
	if sameHTTPOrigin("http://127.0.0.1:1234", "https://127.0.0.1:1234") {
		t.Fatal("sameHTTPOrigin() accepted different schemes")
	}
	if sameHTTPOrigin("http://127.0.0.1:1234", "http://127.0.0.1:1235") {
		t.Fatal("sameHTTPOrigin() accepted different ports")
	}
}

func TestWriteDownloadFileStreamsAndReplacesDestination(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "session.zip")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeDownloadFile(destination, strings.NewReader("new archive bytes")); err != nil {
		t.Fatalf("writeDownloadFile() error = %v", err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new archive bytes" {
		t.Fatalf("download contents = %q", contents)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "session.zip" {
		t.Fatalf("temporary download file was not cleaned up: %#v", entries)
	}
}

func TestWriteDownloadFileRemovesPartialFileOnReadError(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "failed.zip")
	if err := writeDownloadFile(destination, failingDownloadReader{}); err == nil {
		t.Fatal("writeDownloadFile() succeeded for a failing reader")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists after failed download: %v", err)
	}
}

func TestDownloadEndpointCanBeFetchedThroughHTTPClient(t *testing.T) {
	server := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/session.export" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		response.Header().Set("Content-Disposition", `attachment; filename="session.zip"`)
		_, _ = response.Write([]byte("zip bytes"))
	}))
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/session.export", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "zip bytes" {
		t.Fatalf("body = %q", body)
	}
}

func TestDownloadInterceptorUsesDetachedAnchorClickPath(t *testing.T) {
	if !strings.Contains(downloadInterceptorScript, "originalAnchorClick.call(this)") {
		t.Fatal("download interceptor does not preserve ordinary anchor.click()")
	}
	if !strings.Contains(downloadInterceptorScript, "anchor.download || \"\"") {
		t.Fatal("download interceptor does not forward the requested filename")
	}
}

func TestDownloadInterceptorScriptHasNativeBridgeAndCancellation(t *testing.T) {
	for _, fragment := range []string{
		"a[download]",
		"HTMLAnchorElement.prototype.click",
		"preventDefault",
		"chrome.webview.postMessage",
		"webkit.messageHandlers.external",
		"dsh-desktop-download",
	} {
		if !strings.Contains(downloadInterceptorScript, fragment) {
			t.Errorf("download interceptor script is missing %q", fragment)
		}
	}
}
