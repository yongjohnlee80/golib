package dao

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// txContext is one participant in a [Transaction]: a single database's driver tx,
// or a non-DB resource. commit/rollback are expected to be idempotent.
type txContext interface {
	name() string
	commit() error
	rollback() error
}

// sqlTxContext wraps a database driver transaction.
type sqlTxContext struct {
	n  string
	tx TxConn
}

func (c *sqlTxContext) name() string    { return c.n }
func (c *sqlTxContext) commit() error   { return c.tx.Commit() }
func (c *sqlTxContext) rollback() error { return c.tx.Rollback() }

// resourceContext wraps a non-DB [Resource].
type resourceContext struct {
	n string
	r Resource
}

func (c *resourceContext) name() string    { return c.n }
func (c *resourceContext) commit() error   { return c.r.Commit() }
func (c *resourceContext) rollback() error { return c.r.Rollback() }

// Resource is a non-DB participant in a transaction. Its Rollback runs if the
// transaction rolls back and its Commit if it commits; either may be a no-op
// (e.g. an uploaded file that is kept on commit and deleted on rollback).
type Resource interface {
	Commit() error
	Rollback() error
}

type resourceFuncs struct {
	commitFn, rollbackFn func() error
}

func (r resourceFuncs) Commit() error {
	if r.commitFn != nil {
		return r.commitFn()
	}
	return nil
}

func (r resourceFuncs) Rollback() error {
	if r.rollbackFn != nil {
		return r.rollbackFn()
	}
	return nil
}

// ResourceFunc adapts a commit and a rollback function into a [Resource]. A nil
// function is treated as a no-op.
func ResourceFunc(commit, rollback func() error) Resource {
	return resourceFuncs{commitFn: commit, rollbackFn: rollback}
}

// Transaction is a multi-context unit of work. It binds operations across one or
// more [DataConn]s (and optional non-DB [Resource]s) so they commit or roll back
// together.
//
// A Transaction is single-goroutine: do not share one across goroutines
// concurrently (driver transactions are not concurrency-safe). Background work
// must use its own connection via an unbound DAO (schema.DAO()).
type Transaction struct {
	mu       sync.Mutex
	ctx      context.Context
	conns    map[string]DataConn
	contexts map[string]txContext
	order    []string // commit order = first-touch order (deterministic; fixes F2)
	closed   bool
	twoPhase bool
}

// Begin creates a transaction that MAY span the given connections. No driver
// BEGIN is issued yet; each fires lazily on first use of that connection.
func Begin(ctx context.Context, conns ...DataConn) *Transaction {
	if ctx == nil {
		ctx = context.Background()
	}
	t := &Transaction{
		ctx:      ctx,
		conns:    make(map[string]DataConn, len(conns)),
		contexts: make(map[string]txContext),
	}
	for _, c := range conns {
		if c != nil {
			t.conns[c.Name()] = c
		}
	}
	return t
}

// RunTx runs fn inside a transaction over conns, committing on success and rolling
// back on error or panic. It is the primary entry point — prefer it to manual
// Begin/Commit/Rollback. On panic it rolls back and re-panics (never swallows).
func RunTx(ctx context.Context, conns []DataConn, fn func(tx *Transaction) error) (err error) {
	tx := Begin(ctx, conns...)
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()
	if e := fn(tx); e != nil {
		_ = tx.Rollback()
		return e
	}
	return tx.Commit()
}

// TwoPhase opts this transaction into true two-phase commit for genuine
// all-or-nothing across multiple databases: Commit prepares every database
// context (phase one), and only when all prepares succeed commits them all
// (phase two). Every participating dialect must report TwoPhaseSupported;
// otherwise Commit fails fast with ErrTwoPhaseUnsupported before anything
// commits. dao/postgres implements the trio via PREPARE TRANSACTION /
// COMMIT PREPARED / ROLLBACK PREPARED (requires the server to have
// max_prepared_transactions > 0).
//
// Operational note: a crash between the phases can leave prepared
// transactions holding locks on the server. If Commit reports pending
// prepared transactions (CommitError.PreparedPending), resolve them with
// COMMIT PREPARED / ROLLBACK PREPARED (inspect pg_prepared_xacts on
// Postgres). This cost is why 2PC is opt-in (ADR-0005 §2.3).
func (t *Transaction) TwoPhase() *Transaction {
	t.mu.Lock()
	t.twoPhase = true
	t.mu.Unlock()
	return t
}

// Register adds a non-DB [Resource] to the transaction under name. It participates
// in commit/rollback in touch order alongside database contexts.
func (t *Transaction) Register(name string, r Resource) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || r == nil {
		return
	}
	if _, ok := t.contexts[name]; ok {
		return
	}
	t.contexts[name] = &resourceContext{n: name, r: r}
	t.order = append(t.order, name)
}

// executorFor returns the TxConn for the named connection, issuing BEGIN on first
// use and caching it. Called by a tx-bound queryDAO at statement time, so every
// statement automatically runs on the transaction (fixes F3).
func (t *Transaction) executorFor(name string) (TxConn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTransactionClosed
	}
	if c, ok := t.contexts[name]; ok {
		sc, ok := c.(*sqlTxContext)
		if !ok {
			return nil, fmt.Errorf("dao: transaction context %q is not a database connection", name)
		}
		return sc.tx, nil
	}
	conn, ok := t.conns[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownConnection, name)
	}
	tx, err := conn.Begin(t.ctx)
	if err != nil {
		return nil, err
	}
	t.contexts[name] = &sqlTxContext{n: name, tx: tx}
	t.order = append(t.order, name)
	return tx, nil
}

// Commit commits every participating context in deterministic touch order. On the
// first failure it stops, rolls back every not-yet-committed context, and returns
// a *CommitError naming the failed context and listing those already durably
// committed (fixes F1 + F2). A Commit after a successful Commit is a no-op.
func (t *Transaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true

	if t.twoPhase {
		if err := t.checkTwoPhase(); err != nil {
			t.rollbackUncommitted(nil)
			return err
		}
		return t.commitTwoPhase()
	}

	var committed []string
	for _, name := range t.order {
		if err := t.contexts[name].commit(); err != nil {
			t.rollbackUncommitted(committed)
			return &CommitError{Failed: name, Err: err, AlreadyDurable: committed}
		}
		committed = append(committed, name)
	}
	return nil
}

// rollbackUncommitted rolls back every context not present in committed, in
// reverse touch order.
func (t *Transaction) rollbackUncommitted(committed []string) {
	for i := len(t.order) - 1; i >= 0; i-- {
		name := t.order[i]
		if !contains(committed, name) {
			_ = t.contexts[name].rollback()
		}
	}
}

// commitTwoPhase runs the two-phase protocol over the touched contexts.
// Database contexts are prepared in touch order (phase one); only when every
// prepare succeeds are they all committed (phase two). Non-DB resources cannot
// prepare, so they commit last, after the databases are durable, and roll back
// whenever the database decision is abort. Called with t.mu held.
func (t *Transaction) commitTwoPhase() error {
	// Phase one: prepare every database context under a generated global id.
	prepared := make(map[string]string) // context name -> gid
	for _, name := range t.order {
		sc, isDB := t.contexts[name].(*sqlTxContext)
		if !isDB {
			continue
		}
		conn := t.conns[name]
		gid := newGID(name)
		if err := conn.Dialect().Prepare(t.ctx, sc.tx, gid); err != nil {
			// Abort: roll back the already-prepared, then everything else in
			// reverse touch order — including this context, whose driver tx is
			// still open (a failed prepare leaves the session transaction
			// aborted, not released), and the resources.
			for pname, pgid := range prepared {
				_ = t.conns[pname].Dialect().RollbackPrepared(t.ctx, t.conns[pname], pgid)
			}
			for i := len(t.order) - 1; i >= 0; i-- {
				n := t.order[i]
				if _, wasPrepared := prepared[n]; wasPrepared {
					continue
				}
				_ = t.contexts[n].rollback()
			}
			return &CommitError{Failed: name, Err: err}
		}
		prepared[name] = gid
	}

	// Phase two: the decision is commit. Attempt every prepared context even if
	// one fails — a failed COMMIT PREPARED leaves that transaction prepared and
	// recoverable, and is reported in PreparedPending for operator resolution.
	var committed []string
	var failed string
	var firstErr error
	pending := make(map[string]string)
	for _, name := range t.order {
		gid, ok := prepared[name]
		if !ok {
			continue
		}
		if err := t.conns[name].Dialect().CommitPrepared(t.ctx, t.conns[name], gid); err != nil {
			if firstErr == nil {
				failed, firstErr = name, err
			}
			pending[name] = gid
			continue
		}
		committed = append(committed, name)
	}
	if firstErr != nil {
		// Databases are (partially) durable; compensating resources roll back.
		for i := len(t.order) - 1; i >= 0; i-- {
			if _, isDB := t.contexts[t.order[i]].(*sqlTxContext); !isDB {
				_ = t.contexts[t.order[i]].rollback()
			}
		}
		return &CommitError{Failed: failed, Err: firstErr, AlreadyDurable: committed, PreparedPending: pending}
	}

	// Databases are durable; commit resources in touch order.
	for _, name := range t.order {
		if _, isDB := t.contexts[name].(*sqlTxContext); isDB {
			continue
		}
		if err := t.contexts[name].commit(); err != nil {
			for i := len(t.order) - 1; i >= 0; i-- {
				n := t.order[i]
				if _, isDB := t.contexts[n].(*sqlTxContext); isDB || contains(committed, n) {
					continue
				}
				_ = t.contexts[n].rollback()
			}
			return &CommitError{Failed: name, Err: err, AlreadyDurable: committed}
		}
		committed = append(committed, name)
	}
	return nil
}

// newGID builds a globally-unique prepared-transaction id: a fixed prefix, the
// context name (for operator recognizability in pg_prepared_xacts), and random
// entropy. Postgres caps gids at 200 bytes; names are truncated to fit.
func newGID(name string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	if len(name) > 100 {
		name = name[:100]
	}
	return "golib-dao-" + name + "-" + hex.EncodeToString(b[:])
}

// checkTwoPhase verifies every database context's dialect supports 2PC.
func (t *Transaction) checkTwoPhase() error {
	for _, name := range t.order {
		if _, ok := t.contexts[name].(*sqlTxContext); ok {
			conn := t.conns[name]
			if conn == nil || !conn.Dialect().TwoPhaseSupported() {
				return fmt.Errorf("%w: connection %q", ErrTwoPhaseUnsupported, name)
			}
		}
	}
	return nil
}

// Rollback rolls back all participating contexts in reverse touch order. It is
// idempotent and a no-op after a successful Commit. It returns the first rollback
// error, if any.
func (t *Transaction) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	var firstErr error
	for i := len(t.order) - 1; i >= 0; i-- {
		if err := t.contexts[t.order[i]].rollback(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// CommitError reports a commit that failed partway. When AlreadyDurable is
// non-empty, those databases committed before the failure and cannot be rolled
// back — the caller must reconcile. With a single connection AlreadyDurable is
// always empty, so a CommitError means nothing was durably written.
//
// PreparedPending is set only on the two-phase path: it maps context names to
// the global ids of transactions that were successfully prepared but whose
// COMMIT PREPARED failed. They remain durable-prepared (holding locks) on the
// server and must be resolved by an operator with COMMIT PREPARED /
// ROLLBACK PREPARED (on Postgres, inspect pg_prepared_xacts).
type CommitError struct {
	Failed          string
	Err             error
	AlreadyDurable  []string
	PreparedPending map[string]string
}

func (e *CommitError) Error() string {
	if len(e.AlreadyDurable) == 0 {
		return fmt.Sprintf("dao: commit failed on %q (nothing durably written): %v", e.Failed, e.Err)
	}
	return fmt.Sprintf("dao: commit failed on %q after durably committing %v: %v", e.Failed, e.AlreadyDurable, e.Err)
}

func (e *CommitError) Unwrap() error { return e.Err }

// contains reports whether ss includes s.
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// txCtxKey keys a *Transaction stored in a context.Context.
type txCtxKey struct{}

// WithTx stores tx in ctx so a downstream schema.OnCtx can bind to it. The
// explicit *Transaction remains the source of truth; this is convenience sugar.
func WithTx(ctx context.Context, tx *Transaction) context.Context {
	return context.WithValue(ctx, txCtxKey{}, tx)
}

// txFromContext returns the *Transaction stored by WithTx, or nil.
func txFromContext(ctx context.Context) *Transaction {
	if ctx == nil {
		return nil
	}
	tx, _ := ctx.Value(txCtxKey{}).(*Transaction)
	return tx
}
