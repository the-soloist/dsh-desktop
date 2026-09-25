package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/the-soloist/dsh-desktop/internal/backend"
)

func TestPortFromURL(t *testing.T) {
	port, err := portFromURL("http://127.0.0.1:3080")
	if err != nil || port != 3080 {
		t.Fatalf("portFromURL() = %d, %v", port, err)
	}
}

func TestLoopbackURL(t *testing.T) {
	if got := loopbackURL(3081); got != "http://127.0.0.1:3081" {
		t.Fatalf("loopbackURL() = %q", got)
	}
}

func TestAutomatedAndRestartLaunchesNeverPromptOrStopExternalDSH(t *testing.T) {
	for _, restart := range []bool{false, true} {
		for _, smoke := range []bool{false, true} {
			t.Run(fmt.Sprintf("restart=%t/smoke=%t", restart, smoke), func(t *testing.T) {
				prompted := false
				choose := externalDSHLaunchChoice(restart, smoke, func(context.Context, []backend.DSHProcess) (externalDSHChoice, error) {
					prompted = true
					return externalDSHCancel, nil
				})
				choice, err := choose(context.Background(), []backend.DSHProcess{{PID: 123}})
				if err != nil {
					t.Fatal(err)
				}
				if restart || smoke {
					if prompted || choice != externalDSHOtherPort {
						t.Fatalf("automated launch prompted=%t, choice=%v", prompted, choice)
					}
				} else if !prompted || choice != externalDSHCancel {
					t.Fatal("normal initial launch bypassed user confirmation")
				}
			})
		}
	}
}

func TestLaunchPreparationChecksProcessesBeforePorts(t *testing.T) {
	for _, choice := range []externalDSHChoice{externalDSHKill, externalDSHOtherPort} {
		t.Run(fmt.Sprint(choice), func(t *testing.T) {
			var steps []string
			preparation := launchPreparation{
				discover: func(context.Context) ([]backend.DSHProcess, error) {
					steps = append(steps, "discover")
					return []backend.DSHProcess{{PID: 123}}, nil
				},
				choose: func(context.Context, []backend.DSHProcess) (externalDSHChoice, error) {
					steps = append(steps, "choose")
					return choice, nil
				},
				stop: func(context.Context, []backend.DSHProcess) error {
					steps = append(steps, "stop")
					return nil
				},
				available: func(port int) bool {
					steps = append(steps, fmt.Sprint(port))
					return port == 3081
				},
			}
			plan, err := preparation.prepare(context.Background(), 3080)
			want := []string{"discover", "choose"}
			if choice == externalDSHKill {
				want = append(want, "stop")
			}
			want = append(want, "3080", "3081")
			if err != nil || plan.port != 3081 || !reflect.DeepEqual(steps, want) {
				t.Fatalf("plan=%+v, steps=%v, err=%v", plan, steps, err)
			}
		})
	}
}

func TestLaunchPreparationStopsOnInspectionOrConfirmationFailure(t *testing.T) {
	for _, stage := range []string{"discover", "cancel", "stop", "shutdown"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("failure")
			preparation := launchPreparation{
				discover: func(context.Context) ([]backend.DSHProcess, error) {
					if stage == "discover" {
						return nil, failure
					}
					return []backend.DSHProcess{{PID: 123}}, nil
				},
				choose: func(context.Context, []backend.DSHProcess) (externalDSHChoice, error) {
					if stage == "cancel" {
						return externalDSHCancel, nil
					}
					if stage == "shutdown" {
						cancel()
					}
					return externalDSHKill, nil
				},
				stop: func(context.Context, []backend.DSHProcess) error {
					if stage != "stop" {
						t.Fatal("unexpected termination")
					}
					return failure
				},
				available: func(int) bool { t.Fatal("checked port before resolving processes"); return true },
			}
			if _, err := preparation.prepare(ctx, 3080); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLaunchPreparationNeverReusesAnUnidentifiedProfile(t *testing.T) {
	discovered := false
	preparation := launchPreparation{
		discover: func(context.Context) ([]backend.DSHProcess, error) { discovered = true; return nil, nil },
		available: func(port int) bool {
			if !discovered {
				t.Fatal("checked ports before discovering processes")
			}
			return port == 3081
		},
	}
	plan, err := preparation.prepare(context.Background(), 3080)
	if err != nil || plan.port != 3081 {
		t.Fatalf("plan=%+v, err=%v", plan, err)
	}
}

func TestLaunchPreparationSkipsOccupiedNonHTTPPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	occupied := listener.Addr().(*net.TCPAddr).Port
	if occupied == 65535 {
		t.Skip("no subsequent port")
	}
	preparation := launchPreparation{
		discover:  func(context.Context) ([]backend.DSHProcess, error) { return nil, nil },
		available: backend.PortAvailable,
	}
	plan, err := preparation.prepare(context.Background(), occupied)
	if err != nil || plan.port <= occupied {
		t.Fatalf("plan=%+v, err=%v", plan, err)
	}
}
