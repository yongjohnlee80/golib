package dao

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/yongjohnlee80/golib/errs"
)

// txContext is one participant in a [Transaction]: a single database's driver tx,
// or a non-DB resource. commit/rollback are expected to be idempotent.
type txContext interface {
	name() string
	commit() error
	rollback() error
}

// dbTxContext is a database participant: a [txContext] with a live
// transaction-scoped executor. The transaction probes for it by assertion so a
// non-DB [Resource] is never mistaken for a database.
type dbTxContext interface {
	txContext
	executor() TxConn
}

// twoPhaseContext is the optional two-phase-commit capability of a database
// participant. It is a capability interface probed by assertion rather than
// methods on [txContext] (KB convention interface-evolution-capability-interfaces),
// and it is what keeps [Transaction] free of any [DataConn]: the participant
// delegates to the connection it carries.
type twoPhaseContext interface {
	twoPhaseSupported() bool
	prepare(ctx context.Context, gid string) error
	commitPrepared(ctx context.Context, gid string) error
	rollbackPrepared(ctx context.Context, gid string) error
}

// sqlTxContext wraps one database's driver transaction together with the
// connection it was opened on. The connection lives here — not on the
// [Transaction] — because the participant is what needs it: to prepare, and to
// commit or roll back a prepared transaction on the pool once the preparing
// session is gone.
type sqlTxContext struct {
	n    string
	conn DataConn
	tx   TxConn
}

// compile-time: *sqlTxContext is a database participant and can two-phase.
var (
	_ dbTxContext     = (*sqlTxContext)(nil)
	_ twoPhaseContext = (*sqlTxContext)(nil)
)

func (c *sqlTxContext) name() string     { return c.n }
func (c *sqlTxContext) commit() error    { return c.tx.Commit() }
func (c *sqlTxContext) rollback() error  { return c.tx.Rollback() }
func (c *sqlTxContext) executor() TxConn { return c.tx }

// twoPhaseSupported is DERIVED from the dialect's type, not declared by it.
// There is no flag that could answer differently from the implementation: the
// dialect either has the prepared-transaction methods or it does not.
func (c *sqlTxContext) twoPhaseSupported() bool {
	_, ok := c.conn.Dialect().(TwoPhaser)
	return ok
}

// unsupportedTwoPhase names the connection whose engine cannot prepare, so the
// error says which participant blocked the commit rather than only that one
// did.
func (c *sqlTxContext) unsupportedTwoPhase() error {
	return fmt.Errorf("%w: connection %q", ErrTwoPhaseUnsupported, c.n)
}

func (c *sqlTxContext) prepare(ctx context.Context, gid string) error {
	tp, ok := c.conn.Dialect().(TwoPhaser)
	if !ok {
		return c.unsupportedTwoPhase()
	}
	return tp.Prepare(ctx, c.tx, gid)
}

// commitPrepared and rollbackPrepared execute on the pool connection — the
// preparing session is gone by design.
func (c *sqlTxContext) commitPrepared(ctx context.Context, gid string) error {
	tp, ok := c.conn.Dialect().(TwoPhaser)
	if !ok {
		return c.unsupportedTwoPhase()
	}
	return tp.CommitPrepared(ctx, c.conn, gid)
}

func (c *sqlTxContext) rollbackPrepared(ctx context.Context, gid string) error {
	tp, ok := c.conn.Dialect().(TwoPhaser)
	if !ok {
		return c.unsupportedTwoPhase()
	}
	return tp.RollbackPrepared(ctx, c.conn, gid)
}

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

// txConfig is the construction-time state a [TxOption] mutates. Begin turns a
// finished config into a [Transaction].
type txConfig struct {
	span     []DataConn          // declared connections, in declaration order
	declared map[string]struct{} // names in span, for dedupe
	twoPhase bool
	err      error // first construction error; becomes Transaction.initErr
}

// TxOption configures a [Transaction] at construction time (see [Begin] and
// [RunTx]). Functional options — not a positional connection list — because a
// transaction's connections are supplied by the participants that join it.
type TxOption func(*txConfig)

// Spanning declares the connections this transaction MAY span. It is needed
// only to write to MORE THAN ONE database in one transaction: a
// single-database transaction takes no option at all, because each
// tx-bound DAO supplies its schema's own connection when it first runs a
// statement. With a span declared, membership is the admission
// gate; without one, the first database to join locks the transaction to
// itself.
//
// Normalization: declaration order is preserved — it is the
// order [TwoPhase] pre-flight failures are reported in. Repeated Spanning
// options merge, and a name declared twice keeps its first connection and
// position, so passing the same connection twice is not an error. A nil
// connection, or a call declaring no connection at all, is a construction
// error: it fails the transaction before any work rather than being silently
// dropped or silently downgraded to an undeclared span.
func Spanning(conns ...DataConn) TxOption {
	return func(c *txConfig) {
		if len(conns) == 0 {
			c.fail(errs.Wrap(errs.ErrInvalidArgument, "dao: Spanning: no connections declared; omit the option for a single-database transaction"))
			return
		}
		for i, conn := range conns {
			if conn == nil {
				c.fail(errs.Wrap(errs.ErrInvalidArgument, "dao: Spanning: nil connection at index %d", i))
				return
			}
			name := conn.Name()
			if c.declared == nil {
				c.declared = make(map[string]struct{}, len(conns))
			}
			if _, dup := c.declared[name]; dup {
				continue
			}
			c.declared[name] = struct{}{}
			c.span = append(c.span, conn)
		}
	}
}

// TwoPhase opts this transaction into true two-phase commit for genuine
// all-or-nothing across multiple databases: Commit prepares every database
// participant (phase one), and only when all prepares succeed commits them all
// (phase two). Every participating dialect must report TwoPhaseSupported;
// otherwise Commit fails fast with ErrTwoPhaseUnsupported before anything
// commits, and — when [Spanning] declares the span — [RunTx] fails before it
// even calls fn. dao/postgres implements the trio via
// PREPARE TRANSACTION / COMMIT PREPARED / ROLLBACK PREPARED (requires the
// server to have max_prepared_transactions > 0).
//
// It is a construction option rather than a method on [Transaction] so the
// commit protocol cannot be flipped mid-flight, after statements have already
// run on a dialect that cannot prepare.
//
// Operational note: a crash between the phases can leave prepared
// transactions holding locks on the server. If Commit reports pending
// prepared transactions (CommitError.PreparedPending), resolve them with
// COMMIT PREPARED / ROLLBACK PREPARED (inspect pg_prepared_xacts on
// Postgres). This cost is why 2PC is opt-in.
func TwoPhase() TxOption {
	return func(c *txConfig) { c.twoPhase = true }
}

// fail records the first construction error.
func (c *txConfig) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// Transaction is a multi-context unit of work. It binds operations across one or
// more [DataConn]s (and optional non-DB [Resource]s) so they commit or roll back
// together.
//
// It holds no [DataConn] itself: every database participant carries the
// connection it was opened on, supplied by the [Schema] whose
// DAO first ran a statement on this transaction.
//
// A Transaction is single-goroutine: do not share one across goroutines
// concurrently (driver transactions are not concurrency-safe). Background work
// must use its own connection via an unbound DAO (schema.DAO()).
type Transaction struct {
	mu       sync.Mutex
	ctx      context.Context
	span     map[string]struct{} // declared allowlist by name; nil = undeclared
	contexts map[string]txContext
	order    []string // commit order = first-touch order (deterministic; fixes F2)
	closed   bool
	twoPhase bool
	initErr  error // construction failure; surfaced before any work
}

// Begin creates a transaction. No driver BEGIN is issued yet: each participating
// connection fires one lazily, on the first statement that touches it, and is
// supplied by that statement's schema.
//
// Pass [Spanning] only to span more than one database, and [TwoPhase] for
// all-or-nothing across them. Begin returns no error — a bad option
// combination is stored and returned by the first join, by Commit, and by
// [RunTx] before it calls fn.
func Begin(ctx context.Context, opts ...TxOption) *Transaction {
	if ctx == nil {
		ctx = context.Background()
	}
	var cfg txConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	t := &Transaction{
		ctx:      ctx,
		contexts: make(map[string]txContext),
		twoPhase: cfg.twoPhase,
		initErr:  cfg.err,
	}
	if len(cfg.span) > 0 {
		t.span = make(map[string]struct{}, len(cfg.span))
		for _, c := range cfg.span {
			t.span[c.Name()] = struct{}{}
		}
	}
	// Two-phase pre-flight ((a)): an early diagnostic over the
	// DECLARED connections, so an impossible transaction performs no side
	// effects. It is not authority — Commit always validates the participants
	// that actually joined ((b)).
	if t.initErr == nil && cfg.twoPhase {
		for _, c := range cfg.span {
			if _, ok := c.Dialect().(TwoPhaser); !ok {
				t.initErr = fmt.Errorf("%w: connection %q", ErrTwoPhaseUnsupported, c.Name())
				break
			}
		}
	}
	return t
}

// RunTx runs fn inside a transaction, committing on success and rolling back on
// error or panic. It is the primary entry point — prefer it to manual
// Begin/Commit/Rollback. On panic it rolls back and re-panics (never swallows).
//
// The transaction's connections come from the schemas used inside fn, so the
// common single-database call needs no option at all:
//
//	err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
//		_, err := artists.On(tx).Set(ArtistName, "X").Insert()
//		return err
//	})
//
// Writing to a second database requires declaring the span:
//
//	err := dao.RunTx(ctx, fn, dao.Spanning(lmConn, goldConn), dao.TwoPhase())
//
// A mis-constructed transaction (see [Spanning], [TwoPhase]) returns its error
// without calling fn at all.
func RunTx(ctx context.Context, fn func(tx *Transaction) error, opts ...TxOption) (err error) {
	tx := Begin(ctx, opts...)
	if tx.initErr != nil {
		return tx.initErr // fail before fn runs any side effect
	}
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

// join enlists conn in the transaction and returns its transaction-scoped
// executor, issuing BEGIN on first use and caching it. The caller — a tx-bound
// queryDAO — supplies the connection from its schema at statement time, so
// every statement automatically runs on the transaction (fixes F3) and the
// transaction needs no connection registry of its own.
func (t *Transaction) join(conn DataConn) (TxConn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTransactionClosed
	}
	if t.initErr != nil {
		return nil, t.initErr // no work on a mis-constructed transaction
	}
	name := conn.Name()
	if c, ok := t.contexts[name]; ok {
		db, ok := c.(dbTxContext)
		if !ok {
			return nil, errs.Wrap(errs.ErrInvalidArgument, "dao: transaction context %q is not a database connection", name)
		}
		return db.executor(), nil
	}
	if err := t.admits(name); err != nil {
		return nil, err
	}
	// First-touch capability gate: a no-transaction connection
	// (e.g. BigQuery) is an error only when actually touched — an untouched no-tx
	// connection in a multi-conn transaction stays unaffected, preserving the lazy
	// model. Neither RunTx nor Spanning pre-rejects connections before fn runs.
	// No capability gate here. A connection whose engine has no interactive
	// transactions reports that from Begin itself, wrapping ErrUnsupported —
	// the gate that used to sit on this line asked a flag the same question
	// the very next call answers, and a second source of truth can only
	// disagree with the first.
	tx, err := conn.Begin(t.ctx)
	if err != nil {
		return nil, err
	}
	t.contexts[name] = &sqlTxContext{n: name, conn: conn, tx: tx}
	t.order = append(t.order, name)
	return tx, nil
}

// admits reports whether a new participant named name may join.
// With a declared span ([Spanning]) membership is the gate. Undeclared, the
// first database to join locks the transaction to itself: a second, different
// database would be a cross-database span the caller never asked for — and a
// non-atomic commit — so it fails typed instead. Registered non-DB [Resource]s
// never gate anything. Called with t.mu held.
func (t *Transaction) admits(name string) error {
	if t.span != nil {
		if _, ok := t.span[name]; !ok {
			return fmt.Errorf("%w: %s (not in the declared span)", ErrUnknownConnection, name)
		}
		return nil
	}
	for _, n := range t.order {
		if _, isDB := t.contexts[n].(dbTxContext); isDB && n != name {
			return fmt.Errorf("%w: %s (transaction is on %s; declare dao.Spanning to span more than one database)",
				ErrUnknownConnection, name, n)
		}
	}
	return nil
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

	// A mis-constructed transaction cannot report success — even one that never
	// touched a database. Registered resources still get their rollback.
	if t.initErr != nil {
		t.rollbackUncommitted(nil)
		return t.initErr
	}

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
// whenever the database decision is abort. Called with t.mu held, after
// checkTwoPhase has proved every database participant can prepare.
func (t *Transaction) commitTwoPhase() error {
	// Phase one: prepare every database context under a generated global id.
	prepared := make(map[string]string) // context name -> gid
	for _, name := range t.order {
		tp, isDB := t.contexts[name].(twoPhaseContext)
		if !isDB {
			continue
		}
		gid := newGID(name)
		if err := tp.prepare(t.ctx, gid); err != nil {
			// Abort: roll back the already-prepared, then everything else in
			// reverse touch order — including this context, whose driver tx is
			// still open (a failed prepare leaves the session transaction
			// aborted, not released), and the resources.
			for pname, pgid := range prepared {
				if ptp, ok := t.contexts[pname].(twoPhaseContext); ok {
					_ = ptp.rollbackPrepared(t.ctx, pgid)
				}
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
		tp := t.contexts[name].(twoPhaseContext)
		if err := tp.commitPrepared(t.ctx, gid); err != nil {
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
			if _, isDB := t.contexts[t.order[i]].(dbTxContext); !isDB {
				_ = t.contexts[t.order[i]].rollback()
			}
		}
		return &CommitError{Failed: failed, Err: firstErr, AlreadyDurable: committed, PreparedPending: pending}
	}

	// Databases are durable; commit resources in touch order.
	for _, name := range t.order {
		if _, isDB := t.contexts[name].(dbTxContext); isDB {
			continue
		}
		if err := t.contexts[name].commit(); err != nil {
			for i := len(t.order) - 1; i >= 0; i-- {
				n := t.order[i]
				if _, isDB := t.contexts[n].(dbTxContext); isDB || contains(committed, n) {
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

// checkTwoPhase verifies every joined database participant can prepare.
//
// It runs on EVERY two-phase Commit, declared span or not: [Begin]'s pre-flight
// inspects the connections the caller DECLARED, while join enlists the
// connection each [Schema] SUPPLIED, and admission is by name —
// so a capable declaration and an incapable participant can share a name. The
// pre-flight is a diagnostic; this is the authority ((b)).
//
// A participant that does not implement [twoPhaseContext] fails exactly like
// one reporting no support: ErrTwoPhaseUnsupported, before anything is
// prepared. Called with t.mu held.
func (t *Transaction) checkTwoPhase() error {
	for _, name := range t.order {
		if _, isDB := t.contexts[name].(dbTxContext); !isDB {
			continue // resources cannot prepare; they commit last
		}
		tp, ok := t.contexts[name].(twoPhaseContext)
		if !ok || !tp.twoPhaseSupported() {
			return fmt.Errorf("%w: connection %q", ErrTwoPhaseUnsupported, name)
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
