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
	"testing/synctest"
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
		{name: "DSH authentication", statusCode: http.StatusUnauthorized, body: "dsh web authentication required; reopen the URL printed by dsh web.", want: ProbeAuthenticationRequired},
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

func TestWaitForReadyAcceptsAuthenticatedDSH(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusUnauthorized, "dsh web authentication required"), nil
		})}
		supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", Client: client})
		if err := supervisor.waitForReady(context.Background(), nil, 100*time.Millisecond, time.Millisecond, 4*time.Millisecond); err != nil {
			t.Fatalf("waitForReady() error = %v", err)
		}
	})
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
	for _, delay := range []time.Duration{0, 4 * time.Millisecond, 20 * time.Millisecond} {
		for _, failure := range []string{"unavailable", "unexpected"} {
			t.Run(delay.String()+"/"+failure, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					const stability = 4 * time.Millisecond
					var recoveredAt time.Time
					requests := 0
					client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						// synctest advances virtual time, independently of CPU load
						// and timer resolution. Slow probes are valid too.
						time.Sleep(delay)
						requests++
						if requests == 2 {
							if failure == "unexpected" {
								return response(http.StatusOK, "initialising"), nil
							}
							return nil, errors.New("temporarily unavailable")
						}
						if requests == 3 {
							recoveredAt = time.Now()
						}
						return response(http.StatusOK, "DeepSeek Harness"), nil
					})}
					supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", PageMarker: "DeepSeek Harness", Client: client})
					process := &Process{done: make(chan struct{})}
					if err := supervisor.waitForReady(context.Background(), process, time.Second, time.Millisecond, stability); err != nil {
						t.Fatal(err)
					}
					if recoveredAt.IsZero() || time.Since(recoveredAt) < stability {
						t.Fatalf("returned before the recovered service was stable: recovery=%v, now=%v", recoveredAt, time.Now())
					}
					if elapsed := time.Since(recoveredAt); elapsed > stability+delay+time.Millisecond {
						t.Fatalf("waited too long after recovery: %v", elapsed)
					}
				})
			})
		}
	}
}

func TestWaitForReadyRejectsUnexpectedService(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "another service"), nil
	})}
	supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", PageMarker: "DeepSeek Harness", Client: client})
	err := supervisor.waitForReady(context.Background(), nil, time.Second, time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "非 DSH") {
		t.Fatalf("waitForReady() error = %v", err)
	}
}

func TestWaitForReadyAllowsManagedServiceToFinishInitialising(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		requests := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			if requests < 3 {
				return response(http.StatusOK, "temporary startup page"), nil
			}
			return response(http.StatusUnauthorized, "dsh web authentication required"), nil
		})}
		process := &Process{done: make(chan struct{})}
		supervisor := NewSupervisor(Config{URL: "http://127.0.0.1:3080", Client: client})
		if err := supervisor.waitForReady(context.Background(), process, 100*time.Millisecond, time.Millisecond, 4*time.Millisecond); err != nil {
			t.Fatalf("waitForReady() error = %v", err)
		}
	})
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

func TestStopCurrentAcceptsSuccessfulForcedFallback(t *testing.T) {
	done := make(chan struct{})
	process := &Process{cmd: &exec.Cmd{Process: &os.Process{Pid: 42}}, done: done}
	supervisor := NewSupervisor(Config{Logger: log.New(io.Discard, "", 0), StopTimeout: time.Millisecond})
	supervisor.active = process
	supervisor.terminate = func(_ int, force bool) error {
		if force {
			close(done)
			return nil
		}
		return errors.New("graceful termination unavailable")
	}
	if err := supervisor.StopCurrent(); err != nil {
		t.Fatalf("StopCurrent() error = %v, want successful forced fallback", err)
	}
}

func TestCloseRejectsFutureStarts(t *testing.T) {
	supervisor := NewSupervisor(Config{Logger: log.New(io.Discard, "", 0)})
	if err := supervisor.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	_, err := supervisor.Start(context.Background(), Launch{RunnerPath: "missing-runner", PackageReference: "example@1.0.0", Workspace: t.TempDir(), Profile: "web", Port: 3080}, io.Discard)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Start() error = %v, want ErrClosed", err)
	}
}
