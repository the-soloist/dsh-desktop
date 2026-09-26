package desktop

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	for _, existing := range []bool{false, true} {
		name := "new destination"
		if existing {
			name = "existing destination"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			destination := filepath.Join(directory, "failed.zip")
			if existing {
				if err := os.WriteFile(destination, []byte("keep original"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			partial := strings.NewReader("partial archive bytes")
			source := io.MultiReader(partial, failingDownloadReader{})
			if err := writeDownloadFile(destination, source); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("writeDownloadFile() error = %v, want read failure", err)
			}
			if partial.Len() != 0 {
				t.Fatal("fixture did not exercise a partial write")
			}
			wantEntries := 0
			if existing {
				wantEntries = 1
				assertDownloadContents(t, destination, "keep original")
			} else if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("destination exists after failed download: %v", err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != wantEntries {
				t.Fatalf("temporary download files remain: %v, %v", entries, err)
			}
		})
	}
}

func TestDownloadToFileUsesAuthenticatedProxy(t *testing.T) {
	var requests atomic.Int32
	server := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet || request.URL.RequestURI() != "/api/session.export?id=session-1" {
			t.Errorf("request = %s %s", request.Method, request.URL)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if cookie, err := request.Cookie("dsh-auth-test"); err != nil || cookie.Value != "download-session" {
			t.Errorf("download authentication = %v, %v", cookie, err)
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Disposition", `attachment; filename="session.zip"`)
		_, _ = response.Write([]byte("zip bytes"))
	}))
	proxy, err := newDSHAuthenticationProxy(server.URL, &http.Cookie{Name: "dsh-auth-test", Value: "download-session"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	destination := filepath.Join(t.TempDir(), "session.zip")
	if err := downloadToFile(ctx, proxy.URL()+"/api/session.export?id=session-1", destination); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("download request count = %d, want 1", requests.Load())
	}
	assertDownloadContents(t, destination, "zip bytes")
}

func TestDownloadToFilePreservesDestinationOnFailure(t *testing.T) {
	for _, failure := range []string{"HTTP error", "truncated body", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			var requests atomic.Int32
			server := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if failure == "HTTP error" {
					response.WriteHeader(http.StatusUnauthorized)
					return
				}
				response.Header().Set("Content-Length", "100")
				_, _ = response.Write([]byte("incomplete archive"))
			}))
			directory := t.TempDir()
			destination := filepath.Join(directory, "session.zip")
			if err := os.WriteFile(destination, []byte("original archive"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if failure == "cancelled" {
				cancel()
			}
			err := downloadToFile(ctx, server.URL+"/api/session.export", destination)
			switch failure {
			case "HTTP error":
				if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
					t.Fatalf("expected HTTP failure, got %v", err)
				}
			case "truncated body":
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("expected truncated download, got %v", err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) || requests.Load() != 0 {
					t.Fatalf("cancellation: requests=%d, err=%v", requests.Load(), err)
				}
			}
			assertDownloadContents(t, destination, "original archive")
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != "session.zip" {
				t.Fatalf("unexpected files after failure: %v, %v", entries, err)
			}
		})
	}
}

func assertDownloadContents(t *testing.T, path, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != want {
		t.Fatalf("download contents = %q, want %q", contents, want)
	}
}
