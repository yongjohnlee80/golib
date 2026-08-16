# ADR-0009 — `golib/server/rpc`: the Go client

- **Status:** Proposed (2026-08-16; authored by ultron-prime for autodb M6.
  ADR-0008 §2.6 deferred the client "until the TUI needs it — likely
  alongside M6"; M6 is here. Lands on the `tui-m6` branch with the widget
  work; same release.)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** amends ADR-0008 §2.6 (removes "no client in v1")
- **Related:** ADR-0008 (transport/codec — the wire contract this speaks),
  autodb ADR-0057 (first consumer: the TUI's client seam)

## 1. Context

autodb's standing rule: every frontend reaches the core through the RPC
client seam, EVEN in-process — that is how single-source-of-truth is
enforced by construction. The M6 TUI therefore needs a real Go client.
autodb's `rpc.Probe` (a hand-rolled one-shot hello) is the degenerate
form; the general client belongs beside the server it mirrors.

## 2. Decision

`server/rpc` gains a `Client` (same package — it shares Message/Codec/
Error and the two sides evolve together):

```go
func Dial(ctx context.Context, addr string, codec Codec, opts ...ClientOption) (*Client, error)
// Options: ClientLogger(logger.Logger), WithDialer(*net.Dialer),
// ClientMaxMessageBytes(int64) (default 16 MiB, inbound window),
// OnNotification(func(method string, params []any)) — served on ONE
// dispatch goroutine in arrival order; a slow handler backpressures reads.

// Call sends one request and blocks for its response or ctx.
// A *Error from the server is returned as that *Error (errors.As-able);
// transport failure poisons the client (all pending + future calls fail
// with the terminal error).
func (c *Client) Call(ctx context.Context, method string, params ...any) (any, error)

// Notify sends a fire-and-forget notification.
func (c *Client) Notify(method string, params ...any) error

func (c *Client) Close() error   // idempotent; fails pending calls with ErrClientClosed
var ErrClientClosed = errors.New("rpc: client closed")
```

Semantics:

- **msgid correlation.** A monotonically increasing uint32 (wrap allowed —
  the pending map is the collision guard: an in-flight id is never
  reused). One reader goroutine demultiplexes responses to per-call
  channels; writes serialize under a mutex through the same
  staged-frame path as the server (encode fully, bounded, then write).
- **Concurrent Calls are the point** — the TUI issues overlapping
  requests (async-only, autodb Objective 24). Per-call ctx cancellation
  abandons the WAIT, not the request; a late response to an abandoned id
  is dropped (logged at debug).
- **Read side is attacker-adjacent in the gate-guard future** (M9 —
  today's peer is localhost autodb, but the client hardens now, not
  later): the same windowReader bound + codec Limits as the server;
  malformed frame or oversized message → terminal error, connection
  closed, pending calls failed. Server-initiated REQUESTS (kind 0) are
  protocol errors for this client (v1 servers never send them): drop +
  log, per R7's classify-or-reject with the tolerant reading msgpack-RPC
  peers expect.
- **No auto-reconnect.** Session state (autodb's hello admission, tokens)
  makes silent reconnection a correctness trap; the consumer owns the
  reconnect-and-rehandshake loop (autodb ADR-0057 specifies it). The
  terminal error is exposed via `Call`'s failures — no extra state API.
- `Shutdown`-side politeness needs nothing new: the server's drain
  already flushes in-flight replies; the client just observes EOF after.

## 3. Consequences

**Easier:** autodb M6/M7 (TUI + Lua spawn/probe logic can later share it),
M9 gate-guard testing; autodb's Probe can eventually collapse onto the
client. **Harder:** the package now owns both ends of the wire — accepted;
it is also what makes an e2e loopback test suite self-contained.

## 4. Files

`server/rpc/client.go`, `client_test.go` (e2e against the real server:
concurrent calls with interleaved responses, per-call cancellation,
notification dispatch order, poisoned-connection semantics, `-race`),
README + ADR-0008 §2.6 amendment note.

## 5. Acceptance criteria

e2e over real TCP against `rpc.New(...)`: N concurrent Calls with
staggered handlers resolve to their own responses (msgid fidelity);
cancelled Call returns ctx.Err() while the connection stays usable; a
server *Error round-trips `errors.As`; notification callback receives
server pushes in order; malformed frame from a hostile fake server fails
all pending calls with one terminal error; Close is idempotent and
unblocks waiters. `-race` clean.
