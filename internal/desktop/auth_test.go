package desktop

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newIPv4TestServer keeps the network tests deterministic on runners where
// IPv6 loopback is disabled (a common Windows/Linux CI configuration).
func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on IPv4 loopback: %v", err)
	}
	server := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: handler},
	}
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestExchangeDSHAuthenticationCookie(t *testing.T) {
	const cookieName = "dsh-auth-test"
	server := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			http.SetCookie(response, &http.Cookie{Name: cookieName, Value: "signed-session", Path: "/", HttpOnly: true})
			response.Header().Set("Location", "/")
			response.WriteHeader(http.StatusSeeOther)
		case "/":
			if cookie, err := request.Cookie(cookieName); err == nil && cookie.Value == "signed-session" {
				response.WriteHeader(http.StatusOK)
				return
			}
			response.WriteHeader(http.StatusUnauthorized)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))

	cookie, err := exchangeDSHAuthenticationCookie(context.Background(), server.URL+"/token", server.URL+"/")
	if err != nil {
		t.Fatalf("exchangeDSHAuthenticationCookie() error = %v", err)
	}
	if cookie.Name != cookieName || cookie.Value != "signed-session" || !cookie.HttpOnly {
		t.Fatalf("cookie = %#v, want %s=signed-session", cookie, cookieName)
	}
}

func TestExchangeDSHAuthenticationCookieRequiresCookie(t *testing.T) {
	server := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			response.WriteHeader(http.StatusSeeOther)
			return
		}
		response.WriteHeader(http.StatusUnauthorized)
	}))

	_, err := exchangeDSHAuthenticationCookie(context.Background(), server.URL+"/token", server.URL+"/")
	if err != errDSHAuthenticationCookieMissing {
		t.Fatalf("error = %v, want %v", err, errDSHAuthenticationCookieMissing)
	}
}

func TestDSHAuthenticationProxyInjectsCookieAndPreservesBackendOrigin(t *testing.T) {
	const cookieName = "dsh-auth-test"
	var targetHost string
	var targetURL string
	target := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Host != targetHost {
			t.Errorf("Host = %q, want %q", request.Host, targetHost)
		}
		if got := request.Header.Get("Origin"); got != targetURL {
			t.Errorf("Origin = %q, want %q", got, targetURL)
		}
		cookie, err := request.Cookie(cookieName)
		if err != nil || cookie.Value != "fresh-session" {
			t.Errorf("authentication cookie = %#v, error = %v", cookie, err)
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("authenticated"))
	}))
	targetURL = target.URL
	targetHost = strings.TrimPrefix(target.URL, "http://")

	proxy, err := newDSHAuthenticationProxy(target.URL, &http.Cookie{Name: cookieName, Value: "fresh-session"})
	if err != nil {
		t.Fatalf("newDSHAuthenticationProxy() error = %v", err)
	}
	defer proxy.Close()

	request, err := http.NewRequest(http.MethodGet, proxy.URL()+"/api/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", proxy.URL())
	request.Header.Set("Cookie", cookieName+"=stale-session; other=value")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("proxy request error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200", response.StatusCode)
	}
}
