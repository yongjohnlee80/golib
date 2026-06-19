package dao

import (
	"context"
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

// TwoPhase opts this transaction into true two-phase commit (PREPARE TRANSACTION /
// COMMIT PREPARED) for genuine all-or-nothing across multiple databases. Every
// participating dialect must report TwoPhaseSupported; otherwise Commit fails fast.
//
// Note: the prepared-transaction execution itself is provided by the driver
// (ADR-0005 §2.3) and lands with the dao/postgres reference driver. With the
// zero-dependency dialects here, TwoPhase().Commit() fails fast as unsupported.
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
		// True PREPARE TRANSACTION / COMMIT PREPARED is provided by the driver
		// (ADR-0005 §2.3). The ordered commit below is the fallback.
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
type CommitError struct {
	Failed         string
	Err            error
	AlreadyDurable []string
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
