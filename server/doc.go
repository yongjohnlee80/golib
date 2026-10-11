// Package server provides the transport-agnostic foundation for server implementations
// across golib. It isolates core lifecycle management, request routing, middleware chaining,
// and connection draining from any specific wire protocol (HTTP, WebSocket, RPC, SFTP).
//
// The core has zero external dependencies and does not import net/http. All routing and
// middleware primitives are parameterized over the transport's handler type H (for example,
// http.Handler in HTTP servers or connection-handling functions in RPC servers).
//
// # Architectural Primitives
//
//   - [Server]: The canonical lifecycle interface (Run, Shutdown, Addr) governing listener
//     ownership, serving loops, and graceful shutdown deadlines.
//   - [Router]: A radix segment-tree router parameterized over handler type H. It enforces
//     strict deterministic route precedence (static segment > parameter capture > wildcard)
//     and distinguishes route-not-found from method-not-allowed via [MatchResult].
//   - [RouteContext]: Thread-safe container capturing path parameters during route matching,
//     providing zero-allocation access during dispatch and defensive copying when needed.
//   - [Chain]: An immutable builder for middleware pipelines ([Middleware]). Chaining methods
//     never mutate the receiver, allowing safe branch derivation across route groups.
//   - [Registry]: Concurrency-safe tracker for live connections and sessions. Supports pre-handshake
//     reservations ([Reservation]) to eliminate race conditions between connection admission
//     and server shutdown.
//   - [Scaffold]: Reusable network accept loop managing listener binding, TLS configuration,
//     per-connection contexts, backoff on accept errors, and registry-coordinated graceful drain.
//
// # Lifecycle and Graceful Drain Guarantees
//
// Server implementations follow a two-phase termination model upon context cancellation:
//  1. Ingress Termination: The listener closes to reject new connections, and [Registry.Reserve]
//     fails, returning protocol-appropriate refusal signals (such as HTTP 503 Service Unavailable).
//  2. Polite Drain with Bounded Deadline: Active sessions implementing [Drainer] receive polite
//     drain requests (such as WebSocket close frames or RPC finish signals), allowing in-flight
//     work to flush until the shutdown context expires, after which unclosed sessions are terminated.
package server
