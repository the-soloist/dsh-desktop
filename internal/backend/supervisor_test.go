package backend

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestProbeIdentifiesDSH(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       ProbeStatus
	}{
		{name: "DSH page", statusCode: http.StatusOK, body: "<title>DeepSeek Harness</title>", want: ProbeReady},
		{name: "unrelated page", statusCode: http.StatusOK, body: "<title>Other app</title>", want: ProbeUnexpected},
		{name: "DSH starting error", statusCode: http.StatusInternalServerError, body: "DeepSeek Harness", want: ProbeUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(test.statusCode, test.body), nil
			})}
			supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", PageMarker: "DeepSeek Harness", Client: client})
			if got := supervisor.Probe(context.Background()); got != test.want {
				t.Fatalf("Probe() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestProbeReportsUnavailableConnection(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}
	supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", PageMarker: "DeepSeek Harness", Client: client})
	if got := supervisor.Probe(context.Background()); got != ProbeUnavailable {
		t.Fatalf("Probe() = %v, want unavailable", got)
	}
}

func TestWaitForReadyRequiresStableIdentity(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 2 {
			return nil, errors.New("temporarily unavailable")
		}
		return response(http.StatusOK, "DeepSeek Harness"), nil
	})}
	supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", PageMarker: "DeepSeek Harness", Client: client})
	if err := supervisor.waitForReady(context.Background(), nil, 100*time.Millisecond, time.Millisecond, 4*time.Millisecond); err != nil {
		t.Fatalf("waitForReady() error = %v", err)
	}
	if requests < 6 {
		t.Fatalf("probe count = %d, want at least 6", requests)
	}
}

func TestWaitForReadyRejectsUnexpectedService(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "another service"), nil
	})}
	supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", PageMarker: "DeepSeek Harness", Client: client})
	err := supervisor.waitForReady(context.Background(), nil, time.Second, time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "not DSH") {
		t.Fatalf("waitForReady() error = %v", err)
	}
}

func TestStopCurrentIsIdempotentForExitedProcess(t *testing.T) {
	done := make(chan struct{})
	close(done)
	process := &Process{done: done}
	supervisor := NewSupervisor(Config{Logger: log.New(io.Discard, "", 0)})
	supervisor.active = process
	if err := supervisor.StopCurrent(); err != nil {
		t.Fatalf("StopCurrent() error = %v", err)
	}
	if supervisor.HasManagedProcess() {
		t.Fatal("supervisor still owns the exited process")
	}
}

func TestStopCurrentReturnsTerminationFailures(t *testing.T) {
	process := &Process{cmd: &exec.Cmd{Process: &os.Process{Pid: 42}}, done: make(chan struct{})}
	supervisor := NewSupervisor(Config{Logger: log.New(io.Discard, "", 0), StopTimeout: time.Millisecond})
	supervisor.active = process
	supervisor.terminate = func(_ int, force bool) error {
		if force {
			return errors.New("force denied")
		}
		return errors.New("terminate denied")
	}
	err := supervisor.StopCurrent()
	if err == nil || !strings.Contains(err.Error(), "terminate denied") || !strings.Contains(err.Error(), "force denied") {
		t.Fatalf("StopCurrent() error = %v", err)
	}
}

func TestCloseRejectsFutureStarts(t *testing.T) {
	supervisor := NewSupervisor(Config{Package: "example", Logger: log.New(io.Discard, "", 0)})
	if err := supervisor.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	_, err := supervisor.Start(context.Background(), "missing-bunx", t.TempDir(), nil, io.Discard)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Start() error = %v, want ErrClosed", err)
	}
}
