// Package ws is golib's WebSocket transport (golib-server): endpoints
// are ordinary routes on the golib/server/http router, wrapped by the same
// middleware as any handler, with a ctx-first Session API.
//
// Lifecycle is honest end-to-end: Handler reserves a slot in the server's
// session Registry BEFORE the handshake (during shutdown new upgrades receive
// a plain HTTP 503), and established sessions drain politely on Shutdown with
// a StatusGoingAway close frame, bounded by the shutdown deadline.
//
// Security defaults: same-origin enforcement (relax deliberately with
// InsecureAllowOrigins), a 1 MiB read limit, and ping/pong keepalive that
// force-ends dead peers.
//
// Concurrency contract: at most one concurrent reader and one concurrent
// writer per Session (the protocol's constraint).
//
// This is the only golib package importing the WebSocket dependency
// (github.com/coder/websocket); the server core stays dependency-free.
package ws
