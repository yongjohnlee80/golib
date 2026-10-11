// Package httpserver provides an idiomatic HTTP transport implementation for the
// golib server subsystem. It is imported as golib/server/http under the package name
// httpserver to prevent naming collisions with standard library net/http.
//
// The package combines standard net/http compatibility with routing and lifecycle
// primitives provided by [golib/server]:
//
// # Architecture and Key Capabilities
//
//   - [Server]: Production-ready HTTP server managing listener binding, standard timeout
//     defaults (read, header read, write, idle, and shutdown grace periods), TLS support,
//     and listener ownership with graceful draining via [Server.Run] and [Server.Shutdown].
//   - Expressive Routing: HTTP verb methods ([Server.Get], [Server.Post], [Server.Put],
//     [Server.Delete], etc.), route groups ([Server.Group]), subtree mounting ([Server.Mount]),
//     and parameter retrieval ([URLParam]) powered by [server.Router].
//   - Middleware Pipelines: Composability via [server.Chain] with built-in production middleware:
//     [Recover] for panic containment, [RequestLogger] for structured audit logging,
//     [RequestID] for distributed tracing correlation, and [Auth] for header validation.
//   - JSON & Error Protocol: Standardized response serialization helpers ([JSON], [Error],
//     [Decode]) maintaining contract compatibility with [golib/request.Error], alongside an
//     error-returning handler signature adapter ([Handler]) with [StatusError].
//   - Testing Doubles: [MockServer] provides an in-memory loopback HTTP test double for fast,
//     deterministic consumer unit testing without opening live network ports.
//
// Transports remain strictly layered: only httpserver imports net/http, while the core
// golib/server package remains protocol-agnostic.
package httpserver
