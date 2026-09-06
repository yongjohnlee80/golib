package postgres

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yongjohnlee80/golib/dao"
)

// The postgres row of the option matrix, and the commit-outcome
// classification. These need no server: option rendering and outcome
// classification are pure functions, and the "refused before BEGIN" property is
// proven by a connection whose pool is nil — reaching the pool would panic.
// The live half of the matrix is in
// postgres_adr0017_integration_test.go.

// --- the option matrix -------------------------------------------------------

// Postgres honors the whole option domain: every cell of its matrix row renders
// to a pgx option set, and nothing is refused as unsupported.
func TestPgTxOptions_FullMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts dao.TxOptions
		want pgx.TxOptions
	}{
		{"default", dao.TxOptions{}, pgx.TxOptions{}},

		{"read only", dao.TxOptions{Access: dao.TxReadOnly}, pgx.TxOptions{AccessMode: pgx.ReadOnly}},
		{"explicit read write", dao.TxOptions{Access: dao.TxReadWrite}, pgx.TxOptions{AccessMode: pgx.ReadWrite}},

		{"read uncommitted", dao.TxOptions{Isolation: dao.TxReadUncommitted}, pgx.TxOptions{IsoLevel: pgx.ReadUncommitted}},
		{"read committed", dao.TxOptions{Isolation: dao.TxReadCommitted}, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}},
		{"repeatable read", dao.TxOptions{Isolation: dao.TxRepeatableRead}, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}},
		{"serializable", dao.TxOptions{Isolation: dao.TxSerializable}, pgx.TxOptions{IsoLevel: pgx.Serializable}},

		{
			"serializable read only",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly},
			pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadOnly},
		},
		{
			"serializable read only deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxDeferrable},
			pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.Deferrable},
		},
		{
			"serializable read only not deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxNotDeferrable},
			pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable},
		},
		{
			"repeatable read, explicit read write",
			dao.TxOptions{Isolation: dao.TxRepeatableRead, Access: dao.TxReadWrite},
			pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadWrite},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := pgTxOptions(tt.opts)
			if err != nil {
				t.Fatalf("pgTxOptions(%+v) = %v, want no error — postgres refuses nothing in the matrix", tt.opts, err)
			}
			if got != tt.want {
				t.Errorf("pgTxOptions(%+v) = %+v, want %+v", tt.opts, got, tt.want)
			}
		})
	}
}

// Malformed options are refused, and refused BEFORE the BEGIN: the connection
// under test has a nil pool, so any attempt to reach the wire would panic
// rather than return.
func TestPgBeginTx_InvalidOptionsRefusedBeforeBegin(t *testing.T) {
	t.Parallel()

	c := &pgxConn{name: "postgres"} // deliberately no pool
	bad := []dao.TxOptions{
		{Access: dao.TxAccess(9)},
		{Isolation: dao.TxIsolation(9)},
		{Deferrable: dao.TxDeferrableMode(9)},
		{Deferrable: dao.TxDeferrable},                                   // without serializable read only
		{Isolation: dao.TxSerializable, Deferrable: dao.TxNotDeferrable}, // without read only
	}
	for _, opts := range bad {
		for _, call := range []struct {
			name string
			run  func() error
		}{
			{"BeginTx", func() error { _, err := c.BeginTx(context.Background(), opts); return err }},
			{"BeginSessionTx", func() error { _, err := c.BeginSessionTx(context.Background(), opts); return err }},
		} {
			err := call.run()
			var invalid *dao.ErrTxOptionInvalid
			if !errors.As(err, &invalid) {
				t.Errorf("%s(%+v) = %v, want *dao.ErrTxOptionInvalid", call.name, opts, err)
			}
			if errors.Is(err, dao.ErrUnsupported) {
				t.Errorf("%s(%+v): malformed input must not read as a capability miss", call.name, opts)
			}
		}
	}
}

// --- commit outcome classification -------------------------------------------

// classifyCommit answers exactly one question — "is it PROVEN that the
// transaction did not commit?" — and the four fault states are its four
// answers. States 2/3 are built here from the driver's own error values; the
// live paths that produce them are exercised in the integration suite.
func TestClassifyCommit_FaultStates(t *testing.T) {
	t.Parallel()

	// State 3 — pgx reports the server answered COMMIT with a ROLLBACK tag.
	t.Run("server-confirmed rollback", func(t *testing.T) {
		t.Parallel()

		err := classifyCommit(pgx.ErrTxCommitRollback)
		assertRolledBack(t, err)
		// The pgx cause stays reachable: a caller that wants to know WHY still
		// can, without the DAL leaking pgx into its outcome vocabulary.
		if !errors.Is(err, pgx.ErrTxCommitRollback) {
			t.Error("the pgx cause was not preserved through Unwrap")
		}
	})

	// State 3, server ErrorResponse — a deferred constraint or a serialization
	// failure raised at COMMIT. The server answered, so nothing committed.
	t.Run("server error response", func(t *testing.T) {
		t.Parallel()

		cause := &pgconn.PgError{Code: "40001", Message: "could not serialize access"}
		err := classifyCommit(cause)
		assertRolledBack(t, err)

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatal("the *pgconn.PgError must stay reachable with errors.As")
		}
		if pgErr.Code != "40001" {
			t.Errorf("SQLSTATE = %q, want 40001", pgErr.Code)
		}
	})

	// State 2 — pgconn proves nothing was written.
	t.Run("proven not written", func(t *testing.T) {
		t.Parallel()

		err := classifyCommit(safeToRetryError{})
		assertRolledBack(t, err)
	})

	// State 4 — the COMMIT went out and the answer did not come back. Anything
	// not proven is unknown; this is the case autodb records as nonterminal.
	t.Run("response lost", func(t *testing.T) {
		t.Parallel()

		cause := &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}
		err := classifyCommit(cause)
		if !errors.Is(err, dao.ErrTxOutcomeUnknown) {
			t.Fatalf("err = %v, want a dao.ErrTxOutcomeUnknown match", err)
		}
		if errors.Is(err, dao.ErrTxRolledBack) {
			t.Error("an unknown outcome must not also read as a definite rollback")
		}
		var opErr *net.OpError
		if !errors.As(err, &opErr) {
			t.Error("the network cause was not preserved")
		}
	})

	// A plain context error carries no proof either way. It reaches
	// classifyCommit only when the cancellation landed mid-finalization — the
	// pre-dispatch case never gets here (see CommitContext).
	t.Run("bare context error is unknown", func(t *testing.T) {
		t.Parallel()

		if err := classifyCommit(context.Canceled); !errors.Is(err, dao.ErrTxOutcomeUnknown) {
			t.Errorf("err = %v, want a dao.ErrTxOutcomeUnknown match", err)
		}
	})
}

func assertRolledBack(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, dao.ErrTxRolledBack) {
		t.Fatalf("err = %v, want a dao.ErrTxRolledBack match", err)
	}
	if errors.Is(err, dao.ErrTxOutcomeUnknown) {
		t.Error("a definite rollback must not also read as an unknown outcome")
	}
}

// safeToRetryError is a stand-in for the pgconn errors that carry a proof of
// non-transmission (contextAlreadyDoneError, connLockError). Those types are
// unexported, but the interface pgconn.SafeToRetry probes for is not, and it is
// the interface the classification actually consults.
type safeToRetryError struct{}

func (safeToRetryError) Error() string     { return "nothing was written" }
func (safeToRetryError) SafeToRetry() bool { return true }

// The stand-in is only as good as its agreement with pgconn: if
// pgconn.SafeToRetry stopped recognizing it, the state-2 test above would be
// asserting against a fiction.
func TestSafeToRetryStandInIsRecognizedByPgconn(t *testing.T) {
	t.Parallel()

	if !pgconn.SafeToRetry(safeToRetryError{}) {
		t.Fatal("pgconn no longer recognizes the SafeToRetry interface this test depends on")
	}
	if pgconn.SafeToRetry(errors.New("plain")) {
		t.Fatal("pgconn reports a plain error as safe to retry; the state-2 discrimination is meaningless")
	}
}
