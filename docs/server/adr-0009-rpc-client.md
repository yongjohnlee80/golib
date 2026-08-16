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
// ClientMaxMessageBytes(int64) — ONE option bounding BOTH directions
// (default 16 MiB): the inbound read window AND the staged outbound
// frame, the latter enforced during serialization by the same capping
// writer the server uses (r3 — a concrete cap with an API, not an
// unstated "independent" bound),
// ClientWriteTimeout(time.Duration) (default 30s, per frame),
// NotificationBuffer(int) (default 128),
// OnNotification(func(method string, params []any)).

// Call sends one request and blocks for its response or ctx.
// A *Error from the server is returned as that *Error (errors.As-able);
// transport failure poisons the client (all pending + future calls fail
// with the terminal error).
func (c *Client) Call(ctx context.Context, method string, params ...any) (any, error)

// Notify sends a fire-and-forget notification, bounded by ctx and the
// write timeout like any other frame.
func (c *Client) Notify(ctx context.Context, method string, params ...any) error

// Done closes exactly once when the client reaches its terminal state —
// transport poison, msgid exhaustion, or Close. Err returns the terminal
// cause after Done is closed (ErrClientClosed for a local Close), nil
// before. The consumer's reconnect loop is DRIVEN by this signal, never
// by polling calls (r2).
func (c *Client) Done() <-chan struct{}
func (c *Client) Err() error

func (c *Client) Close() error   // idempotent; fails pending calls with ErrClientClosed

var (
	ErrClientClosed    = errors.New("rpc: client closed")
	ErrMsgIDExhausted  = errors.New("rpc: msgid space exhausted; reconnect")
)
```

Semantics:

- **msgid correlation — never wrap (r2).** IDs are strictly monotonic
  uint32 for the connection's lifetime. A pending-only collision check is
  insufficient: a cancelled call's abandoned id could be reused after
  wrap and receive the OLD response. On exhaustion (2³² ids — ~4 billion
  calls) the client poisons with `ErrMsgIDExhausted`; the consumer
  reconnects for a fresh id domain. One reader goroutine demultiplexes
  responses to per-call channels; writes serialize under a mutex through
  the same staged-frame path as the server (encode fully, bounded, then
  write).
- **Cancellation bounds admission, network write, and the response wait
  (r3 — the honest scope):** write-lock acquisition selects on ctx; the
  network write of a staged frame is bounded by `ClientWriteTimeout`
  (via write deadline). STAGING itself is synchronous trusted work —
  `Codec.Write` has no context by design — and is bounded structurally
  instead: ctx is checked before staging begins and after it completes,
  and the staged frame is byte-capped (`ClientMaxMessageBytes`) with the
  codec's encoder depth bound underneath. Stated plainly (r3 amendment):
  ctx MAY expire while staging runs; the byte/depth caps bound the
  OUTPUT SHAPE of built-in and contract-compliant codecs, not the
  execution time of arbitrary user codec code — a custom `Codec.Write`
  must itself terminate and respect the capped writer. If ctx expires or
  the write deadline
  fires after a frame MAY have partially reached the wire, the
  connection is poisoned (a half-written frame is unrecoverable stream
  state); expiry before any byte was written returns promptly with the
  connection intact. Response-wait cancellation abandons the WAIT, not
  the request; a late response to an abandoned (never-reused) id is
  dropped and logged at debug.
- **Notifications — bounded queue, never reader-blocking (r2):** the
  reader appends inbound notifications to a bounded queue
  (`NotificationBuffer`) served by ONE dispatch goroutine in arrival
  order. Because the reader never blocks on the callback, **callback
  reentrancy is a supported contract** — a handler may Call, and the
  response is delivered by the unblocked reader. Queue overflow poisons
  the client (documented terminal cause; unbounded memory is not an
  option and blocking the sole reader deadlocks reentrant handlers).
  Each callback invocation runs inside a recover boundary (R16): a panic
  is logged with the method name and poisons the client (a half-executed
  notification handler is undefined consumer state).
- **Hostile-message taxonomy (r2 — one consistent rule):** the inbound
  read path is attacker-adjacent (M9 gate-guard future; hardened now).
  Malformed frames, oversized messages, invalid kinds, and
  server-initiated REQUESTS (kind 0 — a peer contract violation for this
  client) all POISON: terminal error, connection closed, pending calls
  failed. The single tolerated anomaly is a well-formed response whose
  id matches no pending call (the abandoned-wait case above): dropped +
  logged. Continuing past any other violation hides peer drift.
- **No auto-reconnect.** Session state (autodb's hello admission, tokens)
  makes silent reconnection a correctness trap; the consumer owns the
  reconnect-and-rehandshake loop (autodb ADR-0057 specifies it), driven
  by `Done()`/`Err()`.
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
cancelled Call returns ctx.Err() while the connection stays usable (and
poisons instead when cancellation lands mid-write); a server *Error
round-trips `errors.As`; notification callbacks receive server pushes in
order, a REENTRANT callback's nested Call completes, queue overflow and
callback panic each poison with their documented causes; `Done()` closes
exactly once with `Err()` reporting the terminal cause for poison and
Close alike; malformed frame / invalid kind / server-initiated request
from a hostile fake server each fail all pending calls with one terminal
error, while an unknown-id well-formed response is dropped without harm;
Close is idempotent and unblocks waiters. `-race` clean.
