package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yongjohnlee80/golib/dao"
)

// ADR-0017 capabilities for the Postgres driver. Postgres is the one driver
// that can honor all of them: pgx begins with an explicit option set, its
// Commit/Rollback take a context, and its row stream keeps the wire bytes and
// the server's column descriptors.
//
// Compile-time proof that the base contracts are unchanged and that each
// capability is actually satisfied by the concrete type.
var (
	_ dao.DataConn          = (*pgxConn)(nil)
	_ dao.TxConn            = (*pgxTx)(nil)
	_ dao.TxBeginner        = (*pgxConn)(nil)
	_ dao.SessionTxBeginner = (*pgxConn)(nil)
	_ dao.ContextTxConn     = (*pgxTx)(nil)
	_ dao.RawRows           = (*pgxRows)(nil)
)

// BeginTx starts a transaction with opts, satisfying dao.TxBeginner. Options
// are validated before the BEGIN is built, so a malformed option set never
// reaches the wire; Postgres itself refuses nothing in the ADR-0017 §2.2a
// matrix.
func (c *pgxConn) BeginTx(ctx context.Context, opts dao.TxOptions) (dao.TxConn, error) {
	tx, err := c.beginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// BeginSessionTx starts a transaction with opts and returns a
// context-finalizable handle, satisfying dao.SessionTxBeginner. It is the call
// a session-pinned consumer makes (autodb ADR-0074): the capability is
// assertable on this connection before any transaction exists, and the handle
// it returns can be cleaned up after the session's own context is gone.
func (c *pgxConn) BeginSessionTx(ctx context.Context, opts dao.TxOptions) (dao.ContextTxConn, error) {
	tx, err := c.beginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// beginTx is the shared body of the two capability methods.
func (c *pgxConn) beginTx(ctx context.Context, opts dao.TxOptions) (*pgxTx, error) {
	pgOpts, err := pgTxOptions(opts)
	if err != nil {
		return nil, err
	}
	tx, err := c.pool.BeginTx(ctx, pgOpts)
	if err != nil {
		return nil, err
	}
	return &pgxTx{tx: tx, ctx: ctx}, nil
}

// pgTxOptions validates dao options and renders them as pgx's. Postgres
// supports every option in the domain — including READ UNCOMMITTED, which it
// accepts and then behaves as READ COMMITTED — so the only error this can return is
// dao.ErrTxOptionInvalid, and it returns it before any BEGIN is built.
func pgTxOptions(opts dao.TxOptions) (pgx.TxOptions, error) {
	if err := opts.Validate(); err != nil {
		return pgx.TxOptions{}, err
	}
	var out pgx.TxOptions
	switch opts.Access {
	case dao.TxReadOnly:
		out.AccessMode = pgx.ReadOnly
	case dao.TxReadWrite:
		out.AccessMode = pgx.ReadWrite
	}
	switch opts.Isolation {
	case dao.TxReadUncommitted:
		out.IsoLevel = pgx.ReadUncommitted
	case dao.TxReadCommitted:
		out.IsoLevel = pgx.ReadCommitted
	case dao.TxRepeatableRead:
		out.IsoLevel = pgx.RepeatableRead
	case dao.TxSerializable:
		out.IsoLevel = pgx.Serializable
	}
	switch opts.Deferrable {
	case dao.TxDeferrable:
		out.DeferrableMode = pgx.Deferrable
	case dao.TxNotDeferrable:
		out.DeferrableMode = pgx.NotDeferrable
	}
	return out, nil
}

// CommitContext commits with ctx bounding the COMMIT, satisfying
// dao.ContextTxConn. The outcome is classified per ADR-0017 §2.2a rather than
// handed back raw, because "the commit failed" is three different facts and a
// caller keeping an audit trail must not conflate them.
func (t *pgxTx) CommitContext(ctx context.Context) error {
	if t.closed {
		return fmt.Errorf("postgres: %w: commit", dao.ErrTransactionClosed)
	}
	// Fault state 1: the context is already dead, so nothing is dispatched.
	// The raw context error is returned — neither outcome sentinel matches,
	// because no outcome was attempted — and the handle stays open so the
	// caller can roll back with a fresh context.
	if err := ctx.Err(); err != nil {
		return err
	}
	t.closed = true
	if err := t.tx.Commit(ctx); err != nil {
		return classifyCommit(err)
	}
	return nil
}

// RollbackContext rolls back with ctx bounding the ROLLBACK, satisfying
// dao.ContextTxConn. A failed rollback is reported, never swallowed: pgx kills
// the underlying connection on a rollback failure, so the pinned connection is
// discarded rather than returned to the pool in an undefined state.
func (t *pgxTx) RollbackContext(ctx context.Context) error {
	if t.closed {
		return fmt.Errorf("postgres: %w: rollback", dao.ErrTransactionClosed)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.closed = true
	if err := t.tx.Rollback(ctx); err != nil {
		return fmt.Errorf("postgres: rollback failed: %w", err)
	}
	return nil
}

// classifyCommit maps a pgx commit failure onto the ADR-0017 §2.2a outcome
// contract. The question it answers is only ever "is it PROVEN that the
// transaction did not commit?" — anything short of proof is unknown.
func classifyCommit(err error) error {
	switch {
	// Fault state 3: PostgreSQL answered the COMMIT with a ROLLBACK command
	// tag (the transaction was already aborted). Definitely not committed.
	case errors.Is(err, pgx.ErrTxCommitRollback):
		return &txOutcomeError{outcome: dao.ErrTxRolledBack, cause: err}

	// Fault state 3, the other server-confirmed shape: the server responded to
	// the COMMIT with an ErrorResponse (a deferred constraint, a serialization
	// failure). A response means the server processed it and did not commit.
	case isPgError(err):
		return &txOutcomeError{outcome: dao.ErrTxRolledBack, cause: err}

	// Fault state 2: pgconn proves the COMMIT never reached the server — the
	// context was already done at dispatch, the connection lock failed, the
	// conn was already broken. Nothing was written, so nothing committed.
	case pgconn.SafeToRetry(err):
		return &txOutcomeError{outcome: dao.ErrTxRolledBack, cause: err}

	// Fault state 4: the COMMIT went out and the answer did not come back.
	// Unknowable from here.
	default:
		return &txOutcomeError{outcome: dao.ErrTxOutcomeUnknown, cause: err}
	}
}

// isPgError reports whether err carries a server ErrorResponse.
func isPgError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr)
}

// txOutcomeError joins a dao outcome sentinel to the driver cause that produced
// it. It stays unexported deliberately: the ADR-0017 public error surface is
// the two sentinels plus the preserved cause, so callers match the outcome with
// errors.Is and reach pgx/pgconn/net/context details with errors.As — there is
// no third thing to type-assert.
type txOutcomeError struct {
	outcome error
	cause   error
}

// Error implements error.
func (e *txOutcomeError) Error() string {
	return fmt.Sprintf("postgres: %v: %v", e.outcome, e.cause)
}

// Unwrap returns both the outcome sentinel and the driver cause, so errors.Is
// matches the sentinel and errors.As reaches *pgconn.PgError, pgx sentinels,
// and net/context errors underneath.
func (e *txOutcomeError) Unwrap() []error { return []error{e.outcome, e.cause} }
