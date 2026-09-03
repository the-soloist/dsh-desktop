package desktop

import "sync/atomic"

type servicePhase uint32

const (
	serviceIdle servicePhase = iota
	serviceRestartPending
	serviceStarting
	serviceRestarting
	serviceReady
	serviceFailed
	serviceStopped
	serviceQuitting
)

type serviceLifecycle struct {
	phase atomic.Uint32
}

func (lifecycle *serviceLifecycle) current() servicePhase {
	return servicePhase(lifecycle.phase.Load())
}

func (lifecycle *serviceLifecycle) beginInitial() bool {
	return lifecycle.phase.CompareAndSwap(uint32(serviceIdle), uint32(serviceStarting))
}

func (lifecycle *serviceLifecycle) scheduleRestart() bool {
	for {
		current := lifecycle.current()
		switch current {
		case serviceRestartPending, serviceStarting, serviceRestarting, serviceQuitting:
			return false
		}
		if lifecycle.phase.CompareAndSwap(uint32(current), uint32(serviceRestartPending)) {
			return true
		}
	}
}

func (lifecycle *serviceLifecycle) beginRestart() bool {
	return lifecycle.phase.CompareAndSwap(uint32(serviceRestartPending), uint32(serviceRestarting))
}

func (lifecycle *serviceLifecycle) set(next servicePhase) {
	lifecycle.phase.Store(uint32(next))
}
