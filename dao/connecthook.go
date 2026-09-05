package dao

import "context"

// ConnectHook runs on a freshly opened PHYSICAL connection, before that
// connection serves any query. A non-nil error FAILS the connect: the
// connection is closed and never enters the pool.
//
// It exists because a [DataConn] is a POOL. A consumer that needs every
// physical session to satisfy some property — a grammar setting, a
// search_path, a timezone, a statement timeout — cannot establish it once at
// Open, because a connection the pool creates later, or one it replaces after
// a server restart, never saw that setup. Verifying per STATEMENT is the only
// correct alternative without this seam, and it costs a round-trip on every
// statement.
//
// A hook is portable: it receives dao-level types, so one hook body works
// against every driver that accepts one.
//
// Drivers accept a hook at Open, never afterwards — see each driver's
// WithConnectHook or OpenHooked. There is deliberately no way to register one
// on an already-open DataConn: a hook installed after connections exist would
// silently miss them, which is the failure it was added to prevent.
type ConnectHook func(ctx context.Context, c ConnectedConn) error

// ConnectedConn is what a [ConnectHook] may do on the new connection: issue
// statements and read rows.
//
// It is deliberately NARROWER than [DataConn] — no Begin, no Close, no
// Dialect. A hook that opened a transaction on the connection it is
// configuring, or closed it, would be a bug; this type makes both
// unrepresentable rather than documenting them as forbidden.
type ConnectedConn interface {
	Querier
	Execer
}
