package auth

import (
	"net/netip"
	"testing"
)

// TestAnIPv6PeerIsThrottledByItsSlash64: two addresses of one /64 share a counter, so rotating
// through a subscriber's /64 gives no fresh one; another /64 has its own. IPv4, mapped or not, is
// keyed by its address, without the port.
func TestAnIPv6PeerIsThrottledByItsSlash64(t *testing.T) {
	key := func(s string) string { return peerAddress(&Request{Peer: netip.MustParseAddrPort(s)}) }
	if a, b := key("[2001:db8:1:2::5]:443"), key("[2001:db8:1:2:ffff::9]:51000"); a != b {
		t.Errorf("one /64 keyed as %q and %q", a, b)
	}
	if a, b := key("[2001:db8:1:2::5]:443"), key("[2001:db8:1:3::5]:443"); a == b {
		t.Errorf("two /64s share the key %q", a)
	}
	if a, b := key("10.0.0.4:5"), key("[::ffff:10.0.0.4]:6"); a != "10.0.0.4" || b != "10.0.0.4" {
		t.Errorf("IPv4 keyed as %q, and mapped as %q", a, b)
	}
	if a := key("10.0.0.5:5"); a == key("10.0.0.4:5") {
		t.Errorf("two IPv4 peers share the key %q", a)
	}
}
