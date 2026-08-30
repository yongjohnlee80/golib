package dao

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ADR-0017 §2.2, criterion 2 — the typed helpers. The property under test
// throughout is that nothing falls back silently: a helper either does exactly
// what was asked, or says it cannot.

// --- fakes -------------------------------------------------------------------

// capConn is a DataConn that records which begin path was taken. Capabilities
// are attached by wrapping it (txBeginnerConn, sessionConn) so a test can build
// a connection with exactly the capability set it means to probe.
type capConn struct {
	dialect Dialect

	beganPlain bool            // DataConn.Begin was called
	beganOpts  *TxOptions      // TxBeginner.BeginTx was called, with these
	beganSess  *TxOptions      // SessionTxBeginner.BeginSessionTx was called
	tx         *capTx          // handed back by whichever begin ran
	beginErr   error           // forced begin failure
	beginCtx   context.Context // the ctx the begin received
}

func newCapConn(name string) *capConn {
	return &capConn{dialect: namedDialect{name: name}, tx: &capTx{}}
}

func (c *capConn) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("capConn: no queries")
}

func (c *capConn) ExecContext(context.Context, string, ...any) (Result, error) {
	return nil, errors.New("capConn: no execs")
}

func (c *capConn) Dialect() Dialect { return c.dialect }
func (c *capConn) Name() string     { return c.dialect.Name() }
func (c *capConn) Close() error     { return nil }

func (c *capConn) Begin(ctx context.Context) (TxConn, error) {
	c.beganPlain = true
	c.beginCtx = ctx
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return c.tx, nil
}

// began reports whether ANY begin path ran — the "refused before BEGIN"
// assertion in every option-refusal test.
func (c *capConn) began() bool {
	return c.beganPlain || c.beganOpts != nil || c.beganSess != nil
}

// txBeginnerConn adds the TxBeginner capability to capConn.
type txBeginnerConn struct{ *capConn }

func (c txBeginnerConn) BeginTx(ctx context.Context, opts TxOptions) (TxConn, error) {
	c.beganOpts = &opts
	c.beginCtx = ctx
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return c.tx, nil
}

// sessionConn adds the SessionTxBeginner capability, returning a
// context-finalizable handle.
type sessionConn struct{ *capConn }

func (c sessionConn) BeginSessionTx(ctx context.Context, opts TxOptions) (ContextTxConn, error) {
	c.beganSess = &opts
	c.beginCtx = ctx
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return ctxTx{c.tx}, nil
}

// capTx is a bare TxConn: no context finalizers, so the helpers must refuse it.
type capTx struct {
	committed    bool
	rolledBack   bool
	commitCtx    context.Context
	rollbackCtx  context.Context
	finalizeErr  error
	commitCalled int
}

func (t *capTx) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("capTx: no queries")
}

func (t *capTx) ExecContext(context.Context, string, ...any) (Result, error) {
	return nil, errors.New("capTx: no execs")
}

func (t *capTx) Commit() error {
	t.committed = true
	t.commitCalled++
	return t.finalizeErr
}

func (t *capTx) Rollback() error {
	t.rolledBack = true
	return t.finalizeErr
}

// ctxTx adds the ContextTxConn capability to capTx.
type ctxTx struct{ *capTx }

func (t ctxTx) CommitContext(ctx context.Context) error {
	t.commitCtx = ctx
	return t.Commit()
}

func (t ctxTx) RollbackContext(ctx context.Context) error {
	t.rollbackCtx = ctx
	return t.Rollback()
}

// namedDialect is a GenericDialect with a name, so error messages can be
// checked for the driver they blame.
type namedDialect struct {
	GenericDialect
	name string
}

func (d namedDialect) Name() string { return d.name }

// --- BeginConnTx -------------------------------------------------------------

// Zero options are today's behavior, byte for byte: the unchanged
// DataConn.Begin, on a connection with no capability at all.
func TestBeginConnTx_ZeroOptionsTakeThePlainBeginPath(t *testing.T) {
	t.Parallel()

	c := newCapConn("sqlite")
	tx, err := BeginConnTx(context.Background(), c, TxOptions{})
	if err != nil {
		t.Fatalf("BeginConnTx: %v", err)
	}
	if tx != TxConn(c.tx) {
		t.Errorf("got tx %v, want the connection's own", tx)
	}
	if !c.beganPlain {
		t.Error("zero options must take DataConn.Begin")
	}
	if c.beganOpts != nil {
		t.Error("zero options must not take the TxBeginner path")
	}
}

// A driver that cannot express the option is told so — and the BEGIN never
// happens, so the caller never holds a transaction weaker than the one asked
// for (ADR-0017 §2.2a: refusal before BEGIN).
func TestBeginConnTx_NonDefaultWithoutCapabilityIsRefusedBeforeBegin(t *testing.T) {
	t.Parallel()

	for _, opts := range []TxOptions{
		{Access: TxReadOnly},
		{Access: TxReadWrite},
		{Isolation: TxReadUncommitted},
		{Isolation: TxSerializable},
		{Isolation: TxSerializable, Access: TxReadOnly, Deferrable: TxDeferrable},
	} {
		c := newCapConn("sqlite")
		_, err := BeginConnTx(context.Background(), c, opts)
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("%+v: err = %v, want a dao.ErrUnsupported match", opts, err)
		}
		var unsup *ErrTxOptionUnsupported
		if !errors.As(err, &unsup) {
			t.Errorf("%+v: err = %v, want *ErrTxOptionUnsupported", opts, err)
			continue
		}
		if unsup.Driver != "sqlite" {
			t.Errorf("%+v: Driver = %q, want %q", opts, unsup.Driver, "sqlite")
		}
		if unsup.Option == "" {
			t.Errorf("%+v: the refusal must name the option(s)", opts)
		}
		if c.began() {
			t.Errorf("%+v: a BEGIN was issued despite the refusal", opts)
		}
	}
}

// Validation order (ADR-0017 §2.2a): malformed input is reported as malformed
// input even on a connection that would ALSO have failed the capability probe.
// The reverse would tell a caller their driver is limited when in fact they
// typed nonsense.
func TestBeginConnTx_InvalidIsCheckedBeforeUnsupported(t *testing.T) {
	t.Parallel()

	c := newCapConn("sqlite") // no TxBeginner: would be an unsupported miss
	_, err := BeginConnTx(context.Background(), c, TxOptions{Access: TxAccess(9)})

	var invalid *ErrTxOptionInvalid
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *ErrTxOptionInvalid", err)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Error("the invalid-input error must not also read as a capability miss")
	}
	if c.began() {
		t.Error("a BEGIN was issued for a malformed option set")
	}
}

// A capable driver receives the options verbatim — the helper is a probe, not
// a filter.
func TestBeginConnTx_DelegatesToTxBeginner(t *testing.T) {
	t.Parallel()

	base := newCapConn("mysql")
	c := txBeginnerConn{base}
	opts := TxOptions{Access: TxReadOnly, Isolation: TxRepeatableRead}

	ctx := context.Background()
	tx, err := BeginConnTx(ctx, c, opts)
	if err != nil {
		t.Fatalf("BeginConnTx: %v", err)
	}
	if tx != TxConn(base.tx) {
		t.Errorf("got tx %v, want the connection's own", tx)
	}
	if base.beganPlain {
		t.Error("non-default options must not fall back to DataConn.Begin")
	}
	if base.beganOpts == nil {
		t.Fatal("TxBeginner.BeginTx was not called")
	}
	if *base.beganOpts != opts {
		t.Errorf("BeginTx got %+v, want %+v", *base.beganOpts, opts)
	}
	if base.beginCtx != ctx {
		t.Error("BeginTx did not receive the caller's context")
	}
}

// Even on a capable driver, malformed options are refused by the helper before
// the driver is reached.
func TestBeginConnTx_InvalidNeverReachesACapableDriver(t *testing.T) {
	t.Parallel()

	base := newCapConn("postgres")
	c := txBeginnerConn{base}
	_, err := BeginConnTx(context.Background(), c, TxOptions{Deferrable: TxDeferrable})

	var invalid *ErrTxOptionInvalid
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *ErrTxOptionInvalid", err)
	}
	if base.began() {
		t.Error("the driver was reached with a malformed option set")
	}
}

// A begin failure from the driver is passed through untouched: it is not an
// option problem and must not be dressed up as one.
func TestBeginConnTx_DriverErrorPassesThrough(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection refused")
	base := newCapConn("postgres")
	base.beginErr = boom
	if _, err := BeginConnTx(context.Background(), txBeginnerConn{base}, TxOptions{Access: TxReadOnly}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the driver's own error", err)
	}
	base2 := newCapConn("sqlite")
	base2.beginErr = boom
	if _, err := BeginConnTx(context.Background(), base2, TxOptions{}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the driver's own error", err)
	}
}

// --- SessionTxBeginner -------------------------------------------------------

// The capability is assertable on the CONNECTION, before any transaction
// exists. That is the whole reason it exists: a session-capable connection can
// be rejected at the moment it is declared session-capable, instead of at the
// user's first BEGIN (rev 2, MF1).
func TestSessionTxBeginner_AssertableOnTheConnection(t *testing.T) {
	t.Parallel()

	plain := newCapConn("sqlite")
	if _, ok := DataConn(plain).(SessionTxBeginner); ok {
		t.Error("a plain connection must not claim the session capability")
	}

	base := newCapConn("postgres")
	sess, ok := DataConn(sessionConn{base}).(SessionTxBeginner)
	if !ok {
		t.Fatal("a session-capable connection must be assertable before Begin")
	}

	opts := TxOptions{Access: TxReadOnly}
	tx, err := sess.BeginSessionTx(context.Background(), opts)
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	if base.beganSess == nil || *base.beganSess != opts {
		t.Errorf("BeginSessionTx got %+v, want %+v", base.beganSess, opts)
	}
	// The returned handle is context-finalizable by construction — no second
	// probe needed, which is the difference from asserting ContextTxConn.
	if err := tx.CommitContext(context.Background()); err != nil {
		t.Errorf("CommitContext: %v", err)
	}
}

// --- CommitTx / RollbackTx ---------------------------------------------------

// No silent fallback: a handle without context finalizers is refused rather
// than finalized with the context the caller passed being thrown away.
func TestCommitRollbackTx_RequireContextTxConn(t *testing.T) {
	t.Parallel()

	t.Run("commit", func(t *testing.T) {
		t.Parallel()

		tx := &capTx{}
		err := CommitTx(context.Background(), tx)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("err = %v, want a dao.ErrUnsupported match", err)
		}
		if tx.committed {
			t.Error("the context-free Commit was called as a fallback; the deadline would have been discarded")
		}
		if !strings.Contains(err.Error(), "commit") {
			t.Errorf("message %q should name the operation", err.Error())
		}
	})

	t.Run("rollback", func(t *testing.T) {
		t.Parallel()

		tx := &capTx{}
		err := RollbackTx(context.Background(), tx)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("err = %v, want a dao.ErrUnsupported match", err)
		}
		if tx.rolledBack {
			t.Error("the context-free Rollback was called as a fallback")
		}
		if !strings.Contains(err.Error(), "rollback") {
			t.Errorf("message %q should name the operation", err.Error())
		}
	})
}

func TestCommitRollbackTx_ForwardTheCallersContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "cleanup")

	base := &capTx{}
	if err := CommitTx(ctx, ctxTx{base}); err != nil {
		t.Fatalf("CommitTx: %v", err)
	}
	if !base.committed {
		t.Error("CommitContext was not reached")
	}
	if base.commitCtx != ctx {
		t.Error("CommitContext did not receive the caller's context")
	}

	base2 := &capTx{}
	if err := RollbackTx(ctx, ctxTx{base2}); err != nil {
		t.Fatalf("RollbackTx: %v", err)
	}
	if base2.rollbackCtx != ctx {
		t.Error("RollbackContext did not receive the caller's context")
	}
}

// A finalization failure is the handle's own error, forwarded unchanged: the
// helper classifies nothing.
func TestCommitTx_ForwardsTheHandlesError(t *testing.T) {
	t.Parallel()

	boom := errors.New("commit exploded")
	tx := &capTx{finalizeErr: boom}
	if err := CommitTx(context.Background(), ctxTx{tx}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the handle's own error", err)
	}
	if tx.commitCalled != 1 {
		t.Errorf("CommitContext called %d times, want exactly 1", tx.commitCalled)
	}
}

// --- capability probes are one-way ------------------------------------------

// The base interfaces are unchanged, so a plain implementation of DataConn /
// TxConn — an external fake, a pinned consumer's driver — satisfies neither
// capability and is not broken by their existence.
func TestBaseImplementationsClaimNoCapability(t *testing.T) {
	t.Parallel()

	var conn DataConn = newCapConn("legacy")
	if _, ok := conn.(TxBeginner); ok {
		t.Error("a base DataConn must not satisfy TxBeginner")
	}
	if _, ok := conn.(SessionTxBeginner); ok {
		t.Error("a base DataConn must not satisfy SessionTxBeginner")
	}
	var tx TxConn = &capTx{}
	if _, ok := tx.(ContextTxConn); ok {
		t.Error("a base TxConn must not satisfy ContextTxConn")
	}
}
