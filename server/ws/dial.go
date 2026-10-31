package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/coder/websocket"
)

// Conn is a client WebSocket connection, made by [DialUnix]. Its methods are safe for one
// concurrent reader and one concurrent writer (the protocol's constraint, as for [Session]); each
// takes a ctx that bounds that call alone.
type Conn struct {
	c *websocket.Conn
}

// dialConfig is the option-mutated DialUnix configuration.
type dialConfig struct {
	path         string
	subprotocols []string
	readLimit    int64
}

// DialOption configures DialUnix.
type DialOption func(*dialConfig)

// DialPath sets the request path of the handshake (default "/"): the route a server on the socket
// serves its endpoint at.
func DialPath(path string) DialOption {
	return func(c *dialConfig) { c.path = path }
}

// DialSubprotocols lists the subprotocols to offer; the one the server chose is
// [Conn.Subprotocol].
func DialSubprotocols(names ...string) DialOption {
	return func(c *dialConfig) { c.subprotocols = names }
}

// DialReadLimit caps a single message's size (default 1 MiB, as [ReadLimit]'s). An oversize message
// fails the read and closes the connection with StatusMessageTooBig.
func DialReadLimit(bytes int64) DialOption {
	return func(c *dialConfig) { c.readLimit = bytes }
}

// errNotAbsolute is a request path that is not one: it would not name a route.
var errNotAbsolute = errors.New("ws: the dial path must begin with /")

// DialUnix opens a WebSocket connection to the server listening on the unix socket at path: a
// local peer's endpoint, such as another program's control socket. ctx bounds the dial and the
// handshake only; the connection outlives it until Close. The handshake goes to the socket alone,
// never through a proxy, and no TLS is involved.
func DialUnix(ctx context.Context, path string, opts ...DialOption) (*Conn, error) {
	cfg := dialConfig{path: "/", readLimit: 1 << 20}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if len(cfg.path) == 0 || cfg.path[0] != '/' {
		return nil, errNotAbsolute
	}
	var d net.Dialer
	tr := &http.Transport{
		Proxy: nil, // a local socket: never a proxy
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, "unix", path)
		},
	}
	c, _, err := websocket.Dial(ctx, "ws://localhost"+cfg.path, &websocket.DialOptions{
		HTTPClient:   &http.Client{Transport: tr},
		Subprotocols: cfg.subprotocols,
	})
	if err != nil {
		tr.CloseIdleConnections()
		return nil, err
	}
	c.SetReadLimit(cfg.readLimit)
	return &Conn{c: c}, nil
}

// Read returns the next data message.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	t, p, err := c.c.Read(ctx)
	return MessageType(t), p, err
}

// Write sends one data message.
func (c *Conn) Write(ctx context.Context, t MessageType, data []byte) error {
	return c.c.Write(ctx, websocket.MessageType(t), data)
}

// ReadJSON reads the next message and unmarshals it into v.
func (c *Conn) ReadJSON(ctx context.Context, v any) error {
	_, p, err := c.c.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(p, v)
}

// WriteJSON marshals v and sends it as one text message.
func (c *Conn) WriteJSON(ctx context.Context, v any) error {
	p, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.c.Write(ctx, websocket.MessageText, p)
}

// Subprotocol returns the negotiated subprotocol ("" when none).
func (c *Conn) Subprotocol() string { return c.c.Subprotocol() }

// Close performs the close handshake with code and reason, waiting a bounded time for the peer's
// echo.
func (c *Conn) Close(code StatusCode, reason string) error {
	return c.c.Close(websocket.StatusCode(code), reason)
}

// CloseNow closes the connection at once, without the handshake.
func (c *Conn) CloseNow() error { return c.c.CloseNow() }

// CloseStatus is the close code err carries when it is the peer's close (a Read after the peer
// closed), and -1 otherwise.
func CloseStatus(err error) StatusCode { return StatusCode(websocket.CloseStatus(err)) }
