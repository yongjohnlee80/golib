package ipallow

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/yongjohnlee80/golib/auth"
)

func mustPrefix(s string) netip.Prefix { return netip.MustParsePrefix(s) }

func request(peer string, xff ...string) *auth.Request {
	r := &auth.Request{Peer: netip.MustParseAddrPort(peer)}
	if len(xff) > 0 {
		r.Metadata = map[string][]string{"X-Forwarded-For": xff}
	}
	return r
}

func TestVerify_PeerAddress(t *testing.T) {
	t.Parallel()

	f := New([]netip.Prefix{mustPrefix("10.0.0.0/8"), mustPrefix("::1/128")})
	cases := []struct {
		peer    string
		wantErr error
	}{
		{"10.1.2.3:1234", nil},
		{"[::1]:1234", nil},
		{"192.168.1.1:1234", ErrNotAllowed},
		{"[2001:db8::1]:1234", ErrNotAllowed},
	}
	for _, c := range cases {
		_, err := f.Verify(context.Background(), request(c.peer))
		if !errors.Is(err, c.wantErr) {
			t.Errorf("%s: err = %v, want %v", c.peer, err, c.wantErr)
		}
	}
}

// Deny by default: an empty allowlist admits nobody.
func TestVerify_EmptyAllowlistDenies(t *testing.T) {
	t.Parallel()

	f := New(nil)
	if _, err := f.Verify(context.Background(), request("10.0.0.1:1")); !errors.Is(err, ErrEmptyPolicy) {
		t.Errorf("err = %v, want ErrEmptyPolicy", err)
	}
}

// The header is ignored entirely with no trusted-proxy set — otherwise the
// allowlist would be decoration on any directly reachable listener.
func TestVerify_ForwardedHeaderIgnoredWithoutTrustedProxies(t *testing.T) {
	t.Parallel()

	f := New([]netip.Prefix{mustPrefix("10.0.0.0/8")})
	// The attacker claims to be 10.0.0.5; the real peer is not allowed.
	_, err := f.Verify(context.Background(), request("203.0.113.9:443", "10.0.0.5"))
	if !errors.Is(err, ErrNotAllowed) {
		t.Errorf("a spoofed XFF was believed: err = %v, want ErrNotAllowed", err)
	}
}

func TestVerify_TrustedProxyHopSelection(t *testing.T) {
	t.Parallel()

	f := New(
		[]netip.Prefix{mustPrefix("10.0.0.0/8")},
		TrustedProxies(mustPrefix("203.0.113.0/24")),
	)

	t.Run("rightmost untrusted hop is the client", func(t *testing.T) {
		// A client prepends a lie; the real client is the rightmost untrusted
		// entry, appended by the trusted proxy.
		r := request("203.0.113.9:443", "10.0.0.5, 192.0.2.7")
		if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("leftmost (spoofed) value was used: err = %v", err)
		}
	})

	t.Run("a genuine allowed client behind the proxy passes", func(t *testing.T) {
		r := request("203.0.113.9:443", "10.0.0.5")
		if _, err := f.Verify(context.Background(), r); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("trusted hops are skipped from the right", func(t *testing.T) {
		r := request("203.0.113.9:443", "10.0.0.5, 203.0.113.40")
		if _, err := f.Verify(context.Background(), r); err != nil {
			t.Errorf("err = %v, want nil — the trusted hop should be skipped", err)
		}
	})

	t.Run("an untrusted peer's header is still ignored", func(t *testing.T) {
		r := request("198.51.100.7:443", "10.0.0.5")
		if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("err = %v, want ErrNotAllowed", err)
		}
	})
}

func TestVerify_ContributionShape(t *testing.T) {
	t.Parallel()

	f := New([]netip.Prefix{mustPrefix("10.0.0.0/8")})
	c, err := f.Verify(context.Background(), request("10.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "" {
		t.Errorf("Subject = %q, want empty — an address is not an identity", c.Subject)
	}
	if !c.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero — a static observation bounds nothing", c.ExpiresAt)
	}
	if f.Kind() != auth.FactorContextual {
		t.Error("Kind must be contextual")
	}
}

func TestVerify_NoAddress(t *testing.T) {
	t.Parallel()

	f := New([]netip.Prefix{mustPrefix("10.0.0.0/8")})
	if _, err := f.Verify(context.Background(), &auth.Request{}); !errors.Is(err, ErrNoAddress) {
		t.Errorf("err = %v, want ErrNoAddress", err)
	}
	if _, err := f.Verify(context.Background(), nil); !errors.Is(err, ErrNoAddress) {
		t.Errorf("nil request: err = %v, want ErrNoAddress", err)
	}
}

// The whole point of the contextual classification: a policy cannot be
// satisfied by an address alone.
func TestFactor_CannotStandAloneInAPolicy(t *testing.T) {
	t.Parallel()

	f := New([]netip.Prefix{mustPrefix("10.0.0.0/8")})
	if _, err := auth.NewPolicy(auth.Leaf(f)); !errors.Is(err, auth.ErrNoIdentityProof) {
		t.Errorf("ipallow alone must not form a policy: err = %v", err)
	}
	if _, err := auth.NewPolicy(auth.Any(auth.Leaf(f), auth.Leaf(f))); !errors.Is(err, auth.ErrNoIdentityProof) {
		t.Errorf("Any of contextual factors must not form a policy: err = %v", err)
	}
}
