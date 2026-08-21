package auth

import (
	"net"
	"net/netip"
	"testing"
)

type fakeAddr struct{ network, addr string }

func (f fakeAddr) Network() string { return f.network }
func (f fakeAddr) String() string  { return f.addr }

type fakeConn struct {
	net.Conn
	remote net.Addr
}

func (f fakeConn) RemoteAddr() net.Addr { return f.remote }

func TestFromConn(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		addr net.Addr
		want string // "" means the zero AddrPort
	}{
		"tcp4":                  {&net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 1234}, "203.0.113.7:1234"},
		"tcp6":                  {&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 443}, "[2001:db8::1]:443"},
		"v4-mapped is unmapped": {&net.TCPAddr{IP: net.ParseIP("::ffff:198.51.100.4"), Port: 80}, "198.51.100.4:80"},
		"udp":                   {&net.UDPAddr{IP: net.ParseIP("198.51.100.9"), Port: 53}, "198.51.100.9:53"},
		"unix has no address":   {&net.UnixAddr{Name: "/run/x.sock", Net: "unix"}, ""},
		"string form":           {fakeAddr{"tcp", "192.0.2.5:9000"}, "192.0.2.5:9000"},
		"unparsable":            {fakeAddr{"tcp", "not-an-address"}, ""},
		"empty":                 {fakeAddr{"tcp", ""}, ""},
		"nil addr":              {nil, ""},
		"tcp with no IP":        {&net.TCPAddr{Port: 1}, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := FromConn(fakeConn{remote: c.addr}, nil)
			if c.want == "" {
				if got.Peer.IsValid() {
					// A plausible-looking fallback address would be an allowlist
					// bypass, so the zero value is the only safe answer.
					t.Errorf("Peer = %v, want the zero AddrPort", got.Peer)
				}
				return
			}
			if got.Peer != netip.MustParseAddrPort(c.want) {
				t.Errorf("Peer = %v, want %v", got.Peer, c.want)
			}
		})
	}
}

func TestFromConn_NilConnAndCredentials(t *testing.T) {
	t.Parallel()
	r := FromConn(nil, nil)
	if r == nil {
		t.Fatal("FromConn(nil, nil) must still return a usable Request")
	}
	if r.Peer.IsValid() {
		t.Error("a nil connection has no peer")
	}
	creds := map[string]Secret{"token": NewSecret("t")}
	r = FromConn(fakeConn{remote: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 1}}, creds)
	if r.Credentials["token"].Reveal() != "t" {
		t.Error("credentials were not carried through")
	}
	if r.TLS != nil {
		t.Error("FromConn must not invent TLS state; that is auth/mtls's job")
	}
}
