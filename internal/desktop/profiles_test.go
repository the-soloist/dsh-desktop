package desktop

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/the-soloist/dsh-desktop/internal/backend"
	"github.com/the-soloist/dsh-desktop/internal/profile"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestObsoleteBackendExitKeepsNewProfileAuthentication(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("dsh-auth-profile")
		if err != nil || cookie.Value != "new-profile" {
			t.Errorf("new authentication session was lost: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	proxy, err := newDSHAuthenticationProxy(upstream.URL, &http.Cookie{Name: "dsh-auth-profile", Value: "new-profile"})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	c := &controller{backend: backend.NewSupervisor(backend.Config{}), authenticationProxy: proxy}
	c.service.set(serviceReady)
	c.onBackendExit(&backend.Process{}) // No longer the supervisor's active process.
	if c.authenticationProxy != proxy || c.service.current() != serviceReady {
		t.Fatal("old process exit replaced the new profile session")
	}
	response, err := http.Get(proxy.URL())
	if err != nil {
		t.Fatalf("new profile proxy was closed: %v", err)
	}
	response.Body.Close()
}

func TestProfileActionsOnlyEnabledInSettledStates(t *testing.T) {
	for _, phase := range []servicePhase{serviceIdle, serviceStarting, serviceRestartPending, serviceRestarting, serviceReady, serviceFailed, serviceStopped, serviceQuitting} {
		want := phase == serviceReady || phase == serviceFailed || phase == serviceStopped
		if profileActionsEnabled(phase) != want {
			t.Fatalf("unexpected profile action state: %v", phase)
		}
	}
}

func TestProfileMenuDefaultAndBusyState(t *testing.T) {
	controller := &controller{profiles: profile.New(""), profileMenu: application.NewMenu()}
	defer controller.profileMenu.Destroy()
	controller.populateProfileMenu()
	item := controller.profileMenu.ItemAt(0)
	if item == nil || item.Label() != "web（待启动）" || !item.Checked() || item.Enabled() {
		t.Fatalf("initial profile menu = %+v", item)
	}
	if err := controller.profiles.Refresh(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := controller.profiles.MarkReady(); err != nil {
		t.Fatal(err)
	}
	controller.service.set(serviceReady)
	for _, confirming := range []bool{false, true} {
		controller.profileConfirming.Store(confirming)
		controller.profileMenu.Destroy()
		controller.populateProfileMenu()
		item := controller.profileMenu.ItemAt(0)
		if item.Label() != "web" || !item.Checked() || item.Enabled() == confirming {
			t.Fatalf("settled menu incorrect while confirming=%v", confirming)
		}
	}
}
