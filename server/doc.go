// Package server is the shared, transport-agnostic core of golib's server
// subsystem. It provides building blocks that any transport (HTTP, WebSocket,
// SFTP, …) reuses, generic over the transport's handler type H:
//
//   - [Router] — a tree-based router (static > param > wildcard precedence) whose
//     [Router.Match] returns a [MatchResult] distinguishing not-found from
//     method-not-allowed.
//   - [Chain] — an immutable, builder-pattern middleware chain ([Middleware]).
//   - [Server] — the lifecycle contract a transport server implements.
//   - [RouteContext] — the path parameters captured by a match.
//
// The core has zero external dependencies and imports no net/http; the HTTP
// transport lives in golib/server/http (package httpserver). See the
// golib-server ADRs for the design.
package server
