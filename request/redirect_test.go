package request

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// seenHeaders is the last request's headers a headersSeen server was sent.
type seenHeaders struct {
	mu sync.Mutex
	h  http.Header
}

func (s *seenHeaders) Get(k string) string { s.mu.Lock(); defer s.mu.Unlock(); return s.h.Get(k) }

// headersSeen serves 200 and records the request headers it was sent.
func headersSeen(t *testing.T) (*httptest.Server, *seenHeaders) {
	t.Helper()
	seen := &seenHeaders{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.mu.Lock()
		seen.h = r.Header.Clone()
		seen.mu.Unlock()
	}))
	t.Cleanup(s.Close)
	return s, seen
}

func redirectTo(t *testing.T, target string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/here" {
			http.Redirect(w, r, "/landed", http.StatusFound)
			return
		}
		if r.URL.Path == "/landed" {
			w.Header().Set("X-Seen-Key", r.Header.Get("X-Api-Key"))
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
	}))
	t.Cleanup(s.Close)
	return s
}

var callerHeaders = map[string]string{
	"Authorization": "Bearer t0ken", "X-Api-Key": "k3y", "X-Custom-Credential": "c",
	"Accept": "application/json", "Accept-Language": "en", "Content-Type": "application/json", "User-Agent": "golib-test",
}

// TestDo_ARedirectToAnotherOriginKeepsNoCredential: a redirect to another origin (another port
// here) arrives with only the caller's Accept, Accept-Language, Content-Type and User-Agent: an
// Authorization, Basic auth, an X-API-Key and any other header the caller set are dropped. A
// redirect within the origin keeps them.
func TestDo_ARedirectToAnotherOriginKeepsNoCredential(t *testing.T) {
	t.Parallel()
	other, seen := headersSeen(t)
	from := redirectTo(t, other.URL+"/target")
	for _, basic := range []bool{false, true} {
		p := &Params{Method: http.MethodGet, Url: from.URL + "/go", Headers: map[string]string{}}
		for k, v := range callerHeaders {
			p.Headers[k] = v
		}
		if basic {
			delete(p.Headers, "Authorization")
			p.Username, p.Password = "u", "p"
		}
		if err := Do(context.Background(), p, nil); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"Authorization", "X-Api-Key", "X-Custom-Credential"} {
			if v := seen.Get(k); v != "" {
				t.Errorf("basic %v: %s %q reached another origin", basic, k, v)
			}
		}
		for _, k := range []string{"Accept", "Accept-Language", "Content-Type", "User-Agent"} {
			if seen.Get(k) != callerHeaders[k] {
				t.Errorf("basic %v: %s %q, want the caller's %q kept", basic, k, seen.Get(k), callerHeaders[k])
			}
		}
	}
	p := &Params{Method: http.MethodGet, Url: from.URL + "/here", Headers: map[string]string{"X-Api-Key": "k3y"}}
	if err := Do(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.Response.Header.Get("X-Seen-Key"); got != "k3y" {
		t.Errorf("a redirect within the origin: X-Api-Key %q, want it kept", got)
	}
}

// TestDo_RedirectsStopAtTen: a redirect loop ends after 10 hops, as the stdlib's policy did.
func TestDo_RedirectsStopAtTen(t *testing.T) {
	t.Parallel()
	var hops atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	t.Cleanup(s.Close)
	err := Do(context.Background(), &Params{Method: http.MethodGet, Url: s.URL}, nil)
	if err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Fatalf("a redirect loop: %v, want it stopped after 10", err)
	}
	if n := hops.Load(); n != 10 {
		t.Errorf("%d requests, want 10", n)
	}
}

// TestDo_TheTransportIsBounded: the shared transport has the stdlib default's dial, TLS-handshake
// and idle-connection bounds and environment proxy, where the zero transport had none.
func TestDo_TheTransportIsBounded(t *testing.T) {
	t.Parallel()
	if defaultTransport.Proxy == nil || defaultTransport.DialContext == nil ||
		defaultTransport.TLSHandshakeTimeout <= 0 || defaultTransport.IdleConnTimeout <= 0 {
		t.Errorf("transport: proxy %v, dial %v, TLS handshake %v, idle %v; want all set",
			defaultTransport.Proxy != nil, defaultTransport.DialContext != nil,
			defaultTransport.TLSHandshakeTimeout, defaultTransport.IdleConnTimeout)
	}
	if guardedTransport.Proxy != nil {
		t.Error("the DenyPrivate transport uses a proxy, where the dial check cannot see the target")
	}
}

// TestDo_DenyPrivateRefusesThisHostsNetwork: with DenyPrivateAddresses a request to a loopback
// address is refused at the dial; without it, it is served. The dial check refuses each kind of
// non-public address and passes a public one.
func TestDo_DenyPrivateRefusesThisHostsNetwork(t *testing.T) {
	t.Parallel()
	local, _ := headersSeen(t)
	if err := Do(context.Background(), &Params{Method: http.MethodGet, Url: local.URL}, nil); err != nil {
		t.Fatalf("no guard, loopback: %v", err)
	}
	err := Do(context.Background(), &Params{Method: http.MethodGet, Url: local.URL}, nil, DenyPrivateAddresses())
	if !errors.Is(err, ErrPrivateAddress) {
		t.Errorf("DenyPrivate, loopback: %v, want ErrPrivateAddress", err)
	}
	for _, addr := range []string{"10.0.0.1:80", "192.168.1.1:443", "169.254.169.254:80", "[::1]:80", "[fe80::1]:80", "0.0.0.0:80", "[fd00::1]:80"} {
		if err := refusePrivate("tcp", addr, nil); !errors.Is(err, ErrPrivateAddress) {
			t.Errorf("%s: %v, want refused", addr, err)
		}
	}
	for _, addr := range []string{"93.184.216.34:443", "[2606:2800:220:1::1]:443"} {
		if err := refusePrivate("tcp", addr, nil); err != nil {
			t.Errorf("%s, a public address: %v", addr, err)
		}
	}
}
