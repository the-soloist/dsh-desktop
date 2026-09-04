package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

var errDSHAuthenticationCookieMissing = errors.New("DSH authentication response did not include a dsh-auth cookie")

// exchangeDSHAuthenticationCookie performs the token exchange outside the
// WebView. DSH deliberately returns a 303 with an HttpOnly cookie, so a normal
// HTTP client is the reliable way to obtain the cookie without exposing a
// transient 401 page to the user.
func exchangeDSHAuthenticationCookie(ctx context.Context, tokenURL, baseURL string) (*http.Cookie, error) {
	client := &http.Client{
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create DSH authentication request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("exchange DSH authentication token: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("DSH authentication endpoint returned HTTP %d", response.StatusCode)
	}

	var authenticationCookie *http.Cookie
	for _, cookie := range response.Cookies() {
		if strings.HasPrefix(cookie.Name, "dsh-auth-") {
			authenticationCookie = cookie
			break
		}
	}
	if authenticationCookie == nil {
		return nil, errDSHAuthenticationCookieMissing
	}

	probeRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create DSH cookie probe: %w", err)
	}
	probeRequest.AddCookie(authenticationCookie)
	probeResponse, err := client.Do(probeRequest)
	if err != nil {
		return nil, fmt.Errorf("probe DSH authentication cookie: %w", err)
	}
	defer probeResponse.Body.Close()
	_, _ = io.Copy(io.Discard, probeResponse.Body)
	if probeResponse.StatusCode < http.StatusOK || probeResponse.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("DSH cookie probe returned HTTP %d", probeResponse.StatusCode)
	}
	return authenticationCookie, nil
}

// dshAuthenticationProxy provides the WebView with an authenticated origin
// without asking the WebView to process the launch token. Wails does not
// expose the native cookie stores consistently on all three platforms, so the
// proxy injects the network-exchanged HttpOnly cookie into every request that
// leaves the WebView. The browser sees one local HTTP origin for the session,
// while DSH continues to receive its original Host and Origin authorities.
type dshAuthenticationProxy struct {
	server *http.Server
	url    string
	once   sync.Once
}

func newDSHAuthenticationProxy(targetURL string, authenticationCookie *http.Cookie) (*dshAuthenticationProxy, error) {
	if authenticationCookie == nil || authenticationCookie.Name == "" {
		return nil, errors.New("DSH authentication proxy requires a cookie")
	}
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid DSH target URL %q", targetURL)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for DSH WebView proxy: %w", err)
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(request *http.Request) {
		originalDirector(request)
		// DSH signs the cookie against the authority in Host. Preserve the
		// backend authority even though the browser connects to the proxy.
		request.Host = target.Host
		if origin := request.Header.Get("Origin"); origin != "" {
			request.Header.Set("Origin", target.Scheme+"://"+target.Host)
		}
		injectDSHAuthenticationCookie(request, authenticationCookie)
	}
	proxyURL := "http://" + listener.Addr().String()
	reverseProxy.ModifyResponse = func(response *http.Response) error {
		// Keep redirects inside the proxy origin. DSH normally emits relative
		// redirects, but this also handles an absolute backend Location safely.
		location := response.Header.Get("Location")
		if location == "" {
			return nil
		}
		redirect, parseErr := url.Parse(location)
		if parseErr != nil || redirect.Host == "" || !strings.EqualFold(redirect.Host, target.Host) {
			return nil
		}
		redirect.Scheme = "http"
		redirect.Host = listener.Addr().String()
		response.Header.Set("Location", redirect.String())
		return nil
	}

	proxy := &dshAuthenticationProxy{
		server: &http.Server{Handler: reverseProxy},
		url:    proxyURL,
	}
	go func() {
		if serveErr := proxy.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			// Serve errors are observed by requests as connection failures. There
			// is no second error channel to leak into the startup state machine.
		}
	}()
	return proxy, nil
}

func injectDSHAuthenticationCookie(request *http.Request, authenticationCookie *http.Cookie) {
	cookies := request.Cookies()
	parts := make([]string, 0, len(cookies)+1)
	for _, cookie := range cookies {
		if cookie.Name == authenticationCookie.Name {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	parts = append(parts, authenticationCookie.Name+"="+authenticationCookie.Value)
	request.Header.Set("Cookie", strings.Join(parts, "; "))
}

func (proxy *dshAuthenticationProxy) URL() string {
	if proxy == nil {
		return ""
	}
	return proxy.url
}

func (proxy *dshAuthenticationProxy) Close() error {
	if proxy == nil {
		return nil
	}
	var err error
	proxy.once.Do(func() {
		err = proxy.server.Close()
	})
	return err
}
