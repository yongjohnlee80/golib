// Package rpc is a connection-oriented RPC transport core over a pluggable
// wire Codec (server), built on server.Scaffold: per-connection
// read loop, bounded concurrent dispatch with backpressure, per-request
// contexts cancelled on Shutdown, a pre-dispatch Gate for
// handshake-before-methods policy, per-message size bounds, and polite drain
// (in-flight replies flush before the connection closes).
//
// The first (and reference) Codec is msgpackrpc — the msgpack-RPC framing
// Neovim speaks natively. The Codec seam exists so alternate wire formats
// (e.g. jsonrpc) can reuse the same lifecycle unchanged.
//
// Wire input is treated as attacker-adjacent: a message overrunning
// MaxMessageBytes or failing to frame closes the connection; handler panics
// are isolated to an internal-error reply; dispatch concurrency is bounded
// per connection.
package rpc
