package desktop

import "testing"

func TestServiceLifecycle(t *testing.T) {
	var lifecycle serviceLifecycle
	if !lifecycle.beginInitial() || lifecycle.current() != serviceStarting {
		t.Fatalf("initial transition ended in %v", lifecycle.current())
	}
	if lifecycle.scheduleRestart() {
		t.Fatal("restart was scheduled while startup was active")
	}
	lifecycle.set(serviceReady)
	if !lifecycle.scheduleRestart() || lifecycle.current() != serviceRestartPending {
		t.Fatalf("restart scheduling ended in %v", lifecycle.current())
	}
	if !lifecycle.beginRestart() || lifecycle.current() != serviceRestarting {
		t.Fatalf("restart transition ended in %v", lifecycle.current())
	}
	lifecycle.set(serviceQuitting)
	if lifecycle.scheduleRestart() {
		t.Fatal("restart was scheduled while quitting")
	}
}

func TestLateExitDoesNotOverrideProfileSwitch(t *testing.T) {
	for _, phase := range []servicePhase{serviceIdle, serviceStarting, serviceRestartPending, serviceRestarting, serviceFailed, serviceQuitting} {
		var lifecycle serviceLifecycle
		lifecycle.set(phase)
		if lifecycle.markStopped() || lifecycle.current() != phase {
			t.Fatalf("exit notification changed phase %v to %v", phase, lifecycle.current())
		}
	}
	var lifecycle serviceLifecycle
	lifecycle.set(serviceReady)
	if !lifecycle.markStopped() || lifecycle.current() != serviceStopped {
		t.Fatal("active service exit was not reported")
	}
}
