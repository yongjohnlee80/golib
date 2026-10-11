// Package rpc provides a high-performance, connection-oriented RPC transport
// built on [golib/server.Scaffold] and decoupled from physical wire serialization via
// the pluggable [Codec] interface.
//
// # Architectural Model
//
//   - Transport Lifecycle: Built directly on [server.Scaffold] and [server.Registry],
//     managing listener binding, connection acceptance, worker concurrency, and polite
//     shutdown draining.
//   - Pluggable Framing Codecs: The wire framing layer is separated from server logic via
//     [Codec]. The reference implementation is msgpackrpc (compatible with the Msgpack-RPC
//     framing spoken natively by Neovim), allowing alternative wire protocols (such as JSON-RPC)
//     to share the same session lifecycle without modification.
//   - Dispatch Control & Backpressure: Inbound requests run through bounded concurrency workers
//     per connection, applying backpressure to prevent runaway resource exhaustion under load.
//   - Handshake Gate: An optional pre-dispatch [Gate] enforces protocol handshakes or authentication
//     prerequisites before exposing registered RPC method handlers.
//   - Defensive Wire Handling: Inbound frame streams are validated against configurable size limits
//     ([MaxMessageBytes]); framing overruns immediately terminate the connection, and handler
//     panics are caught and converted into internal error responses without crashing the server process.
//   - Polite Connection Drain: During server shutdown, existing connections stop accepting new requests
//     while allowing in-flight RPC responses to flush before socket termination.
package rpc
