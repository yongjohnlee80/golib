package dao

import (
	"context"
	"errors"
	"fmt"
)

// Optional transaction capabilities. [DataConn] and [TxConn]
// are unchanged — a published interface others implement is never grown (KB
// convention interface-evolution-capability-interfaces; golib policy: existing
// capabilities are not broken for the rest of the consumers). Options-bearing
// BEGIN and context-bounded finalization arrive as separate interfaces a driver
// opts into, probed by type assertion at the call site or, preferably, through
// the typed helpers below.
//
// The helpers never silently fall back. A missing capability is reported as
// [ErrUnsupported] (or [ErrTxOptionUnsupported], which matches it), because the
// two possible "quiet" fallbacks are both lies: beginning without the requested
// options would hand back a weaker transaction than asked for, and finalizing
// without the caller's context would discard the deadline that made the caller
// pass one.

// TxBeginner is an optional [DataConn] capability: beginning a transaction with
// explicit [TxOptions]. Drivers that cannot express any non-default option do
// not implement it (modernc/sqlite), and drivers that can express only some
// refuse the rest with [ErrTxOptionUnsupported] before the BEGIN is sent
// (mysql — see the ADR-0017 §2.2a matrix).
//
// The returned TxConn is finalized with the base Commit/Rollback unless it also
// satisfies [ContextTxConn].
type TxBeginner interface {
	// BeginTx starts a driver transaction with opts. ctx bounds the BEGIN
	// itself. Option errors are returned before any BEGIN reaches the wire:
	// [ErrTxOptionInvalid] for malformed input, then [ErrTxOptionUnsupported]
	// for an option this driver cannot honor.
	BeginTx(ctx context.Context, opts TxOptions) (TxConn, error)
}

// ContextTxConn is an optional [TxConn] capability: finalizers that take their
// own context, so cleanup after the transaction's original context has been
// cancelled (a session timeout, a closed connection) still has a usable
// deadline.
//
// It is implemented only where the driver can honor it natively — on pgx,
// whose Commit/Rollback take a context. *sql.Tx exposes no context finalizers
// and its BeginTx context owns the transaction's lifetime and auto-rollback, so
// database/sql-backed drivers (mysql, sqlite) honestly do not implement this:
// a pre-check cannot bound an in-flight COMMIT, and a goroutine wrapper would
// race a false completion report.
//
// Outcome contract for the finalizers — see [ErrTxRolledBack] and
// [ErrTxOutcomeUnknown].
type ContextTxConn interface {
	TxConn

	// CommitContext commits, bounding the COMMIT with ctx.
	//
	//   - ctx already cancelled when called: nothing is dispatched; the raw
	//     context error is returned (neither outcome sentinel matches) and the
	//     handle stays OPEN — roll it back with a fresh context.
	//   - the driver proves nothing was written: [ErrTxRolledBack].
	//   - the server confirms a rollback: [ErrTxRolledBack], cause preserved.
	//   - COMMIT was written and the outcome is not provable:
	//     [ErrTxOutcomeUnknown]. The handle is closed and the connection is
	//     discarded, not returned to the pool.
	CommitContext(ctx context.Context) error

	// RollbackContext rolls back, bounding the ROLLBACK with ctx. A cancelled
	// ctx is reported before dispatch and leaves the handle open. A failed
	// rollback returns an observable cleanup error and discards the connection.
	RollbackContext(ctx context.Context) error
}

// SessionTxBeginner is an optional [DataConn] capability combining
// [TxBeginner] and [ContextTxConn]: it begins with options AND returns a handle
// whose finalizers take a context.
//
// It exists so a consumer that REQUIRES both — autodb's session engine, which
// pins one transaction across RPC calls and must clean it up after the session
// context is gone — can assert the requirement on the connection at
// the moment it is marked session-capable, before any Begin. Asserting
// [ContextTxConn] cannot do that: the TxConn only exists after a transaction
// has already started, which is far too late to report "this connection cannot
// host a session".
//
// Only postgres implements it, consistent with the product ruling that
// session-capable connections are PostgreSQL.
type SessionTxBeginner interface {
	// BeginSessionTx starts a driver transaction with opts and returns a
	// context-finalizable handle. Option errors follow [TxBeginner.BeginTx].
	BeginSessionTx(ctx context.Context, opts TxOptions) (ContextTxConn, error)
}

// Transaction finalization outcomes. A commit that fails is
// not one thing: the DAL classifies what is actually known and lets the caller
// map it into its own audit state. Test with errors.Is; the driver's own cause
// (a *pgconn.PgError, a pgx sentinel, a net or context error) stays reachable
// through Unwrap for errors.As.
var (
	// ErrTxRolledBack reports that the transaction DEFINITELY did not commit:
	// the server confirmed a rollback, or the driver proved the COMMIT never
	// reached the wire. It is safe to report the work as not applied.
	ErrTxRolledBack = errors.New("dao: transaction rolled back; it did not commit")

	// ErrTxOutcomeUnknown reports that the COMMIT may have reached the server
	// and its outcome is unknowable from here — the response was lost, or the
	// context was cancelled mid-finalization. The work may or may not be
	// applied; it must not be reported as either. Callers that keep an audit
	// trail record a nonterminal "unknown" state and reconcile out of band.
	ErrTxOutcomeUnknown = errors.New("dao: transaction outcome unknown; the commit may or may not have been applied")
)

// BeginConnTx starts a transaction on c with opts, without the caller writing a
// type assertion.
//
// Zero opts take the unchanged [DataConn.Begin] path, so this is a drop-in for
// existing code. Non-default opts REQUIRE [TxBeginner]: a connection that does
// not implement it gets [ErrTxOptionUnsupported] (matching [ErrUnsupported]) —
// never a silent begin without the options, which would hand back a transaction
// weaker than the one that was asked for. Malformed options are rejected first,
// with [ErrTxOptionInvalid]. Both refusals happen before any BEGIN is sent.
func BeginConnTx(ctx context.Context, c DataConn, opts TxOptions) (TxConn, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if opts.IsDefault() {
		return c.Begin(ctx)
	}
	b, ok := c.(TxBeginner)
	if !ok {
		return nil, &ErrTxOptionUnsupported{Driver: driverName(c), Option: opts.nonDefault()}
	}
	return b.BeginTx(ctx, opts)
}

// CommitTx commits tx with ctx bounding the COMMIT. It REQUIRES
// [ContextTxConn]: a handle without the capability returns an error matching
// [ErrUnsupported] rather than falling back to the context-free
// [TxConn.Commit], which would silently discard ctx's deadline. Legacy callers
// that do not need a bounded commit keep calling tx.Commit() directly.
//
// The outcome contract is [ContextTxConn.CommitContext]'s.
func CommitTx(ctx context.Context, tx TxConn) error {
	c, ok := tx.(ContextTxConn)
	if !ok {
		return fmt.Errorf("%w: context-bounded commit", ErrUnsupported)
	}
	return c.CommitContext(ctx)
}

// RollbackTx rolls tx back with ctx bounding the ROLLBACK. Like [CommitTx] it
// REQUIRES [ContextTxConn] and never falls back to the context-free
// [TxConn.Rollback].
func RollbackTx(ctx context.Context, tx TxConn) error {
	c, ok := tx.(ContextTxConn)
	if !ok {
		return fmt.Errorf("%w: context-bounded rollback", ErrUnsupported)
	}
	return c.RollbackContext(ctx)
}

// driverName reports the dialect name of c for an error message, tolerating a
// connection whose dialect is not yet set (tests, partially-built fakes).
func driverName(c DataConn) string {
	if c == nil {
		return "dao"
	}
	d := c.Dialect()
	if d == nil {
		return "dao"
	}
	return d.Name()
}
