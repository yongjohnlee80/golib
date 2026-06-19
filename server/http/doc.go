// Package httpserver is the HTTP transport for golib's server subsystem. It is the
// import path golib/server/http; the package is named httpserver (not http) so it
// never collides with the standard library net/http.
//
// It layers chi-style ergonomics over the generic golib/server core:
//
//   - [Server] — a net/http server with functional-options config, safe default
//     timeouts, and a graceful, listener-owning [Server.Run]/[Server.Shutdown]
//     lifecycle.
//   - chi-style routing ([Server.Get]/[Server.Post]/…, [Server.Group],
//     [Server.Mount], [URLParam]) over a server.Router[http.Handler].
//   - builder-pattern middleware ([Middleware], the server.Chain) plus built-ins
//     [Recover], [RequestLogger], [RequestID], [Auth].
//   - JSON helpers ([JSON], [Error], [Decode]) whose error envelope is
//     JSON-schema-compatible with request.Error, and an error-returning [Handler]
//     adapter with [StatusError].
//   - [MockServer] — a loopback test double for consumer unit tests.
//
// Only this package (and future transports) import a transport library; the core
// stays clean. Dependencies: stdlib net/http + golib/logger + golib/server.
package httpserver
