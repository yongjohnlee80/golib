package auth

import (
	"net"
	"net/netip"
)

// FromConn projects a connection into a [Request], for the transports that are
// not HTTP — `server/rpc`, `server/ws`, or a plain listener.
//
// It takes only `net`, so the core stays free of framework coupling. TLS state
// is deliberately NOT extracted here: projecting it correctly means refusing to
// carry `PeerCertificates`, which is `auth/mtls`'s job. A caller with a
// TLS connection sets Request.TLS from mtls.FromConnectionState:
//
//	r := auth.FromConn(c, creds)
//	if tc, ok := c.(*tls.Conn); ok {
//	    st := tc.ConnectionState()
//	    r.TLS = mtls.FromConnectionState(&st)
//	}
//
// creds may be nil for a purely contextual policy.
func FromConn(c net.Conn, creds map[string]Secret) *Request {
	r := &Request{Credentials: creds}
	if c == nil {
		return r
	}
	r.Peer = peerOfAddr(c.RemoteAddr())
	return r
}

// peerOfAddr extracts an AddrPort from a net.Addr.
//
// An address that cannot be parsed yields the ZERO AddrPort rather than a guess.
// Every address-keyed control must read that as "no address", never as a match —
// a plausible-looking fallback here would be an allowlist bypass.
func peerOfAddr(a net.Addr) netip.AddrPort {
	if a == nil {
		return netip.AddrPort{}
	}
	// The common concrete types carry the address without a string round-trip.
	switch v := a.(type) {
	case *net.TCPAddr:
		if ap := v.AddrPort(); ap.Addr().IsValid() {
			return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
		}
		return netip.AddrPort{}
	case *net.UDPAddr:
		if ap := v.AddrPort(); ap.Addr().IsValid() {
			return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
		}
		return netip.AddrPort{}
	case *net.UnixAddr:
		// A unix socket has no network address. Returning the zero value is
		// correct: the peer is local, and an address-keyed factor has nothing to
		// match on. Authenticate a unix peer by credential, not by address.
		return netip.AddrPort{}
	}
	ap, err := netip.ParseAddrPort(a.String())
	if err != nil {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
