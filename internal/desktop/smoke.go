package desktop

import "time"

func (controller *controller) recordSmokeFailure(failure error) {
	if !smokeTestEnabled() || failure == nil {
		return
	}
	controller.smokeMu.Lock()
	if controller.smokeErr == nil {
		controller.smokeErr = failure
	}
	controller.smokeMu.Unlock()
}

func (controller *controller) smokeFailure() error {
	controller.smokeMu.Lock()
	defer controller.smokeMu.Unlock()
	return controller.smokeErr
}

func (controller *controller) scheduleSmokeFailureExit() {
	if smokeTestEnabled() && controller.smokeScheduled.CompareAndSwap(false, true) {
		time.AfterFunc(time.Second, controller.quit)
	}
}

func (controller *controller) scheduleSmokeSuccess() {
	if !smokeTestEnabled() || !controller.smokeScheduled.CompareAndSwap(false, true) {
		return
	}
	duration := smokeTestDuration()
	closeDelay := time.Second
	if duration <= 2*time.Second {
		closeDelay = duration / 2
	}
	controller.logger.Printf("[smoke] closing window after %s; exiting after %s", closeDelay, duration)
	time.AfterFunc(closeDelay, controller.window.window.Close)
	time.AfterFunc(duration, controller.quit)
}
