// Package ws provides WebSocket endpoint integration for HTTP servers in golib.
// Endpoints are declared as standard route handlers on [httpserver.Server], wrapped
// by common middleware, and exposed through a context-aware [Session] API.
//
// # Lifecycle and Handshake Invariants
//
// The WebSocket lifecycle integrates directly with the server's graceful shutdown protocol:
//   - Admission Pre-Reservation: [Handler] claims a reservation in [server.Registry] BEFORE
//     initiating the HTTP upgrade handshake. If shutdown is already underway, new connections
//     are cleanly rejected with an HTTP 503 Service Unavailable status rather than being accepted
//     and immediately aborted.
//   - Polite Close: When the server shuts down, established sessions drain politely
//     by initiating a close with StatusGoingAway, and the drain loop waits for the active
//     handler to return or for shutdown context cancellation (which force-closes the session).
//
// # Security and Concurrency Guarantees
//
//   - Origin Validation: Enforces same-origin checking by default; cross-origin access must be
//     explicitly enabled using [InsecureAllowOrigins].
//   - Resource Protection: Enforces strict message size limits (1 MiB default) and active
//     ping/pong keepalive probes to clean up dead or unresponsive peers.
//   - Concurrency Model: The underlying connection is safe for at most one concurrent
//     reader and one concurrent writer goroutine (the caller must ensure concurrent readers
//     or writers are not spawned).
//
// # Client
//
// [DialUnix] connects to a WebSocket server on a unix socket (a local peer's endpoint, such as
// another program's control socket) and answers a [Conn] with the Session's data methods; its ctx
// bounds the dial and handshake alone.
//
// This is the only golib package importing the WebSocket dependency
// (github.com/coder/websocket); the server core stays dependency-free.
package ws
