package dao

import "context"

// Dialect captures the SQL differences between databases. There is one Dialect
// per driver, and it is the entire per-driver contract: the DAL engine calls
// only these methods, so adding a database means implementing [DataConn] and
// Dialect once, with no change to the engine, the interfaces, or any entity
// declaration.
type Dialect interface {
	// Name is a short dialect id ("postgres", "mysql", "sqlite").
	Name() string

	// Placeholder renders the nth (1-based) bind placeholder: "$1" (Postgres) or
	// "?" (MySQL/SQLite).
	Placeholder(n int) string

	// MaxBindParams is the maximum number of bind parameters per statement. It
	// drives batch chunking. Postgres: 65535. SQLite: 999 (or 32766 since 3.32).
	// MySQL: effectively packet-bound — use a conservative cap. MSSQL: 2100.
	MaxBindParams() int

	// MaxBatchRows optionally caps rows per batch statement regardless of the
	// parameter count (0 means no extra cap), bounding statement size for planner
	// sanity.
	MaxBatchRows() int

	// QuoteIdent quotes a table or column identifier for the dialect.
	QuoteIdent(ident string) string

	// SupportsReturning reports whether INSERT ... RETURNING <id> is available
	// (Postgres/SQLite yes; MySQL no — fall back to LastInsertId).
	SupportsReturning() bool

	// BuildUpsertSuffix renders the conflict clause appended to an INSERT. With a
	// non-empty conflictCols it renders an upsert (e.g.
	// "ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name"); with no conflictCols
	// it renders the dialect's "do nothing on conflict" clause.
	BuildUpsertSuffix(conflictCols, updateCols []string) string

	// CopySupported reports whether the dialect has a bulk-load fast-path.
	CopySupported() bool

	// Copy performs a bulk load of rows into table(cols). It is implemented only
	// when CopySupported reports true; the engine calls it for large batches. ex
	// is the executor (pool or transaction) the batch is running on.
	Copy(ctx context.Context, ex any, table string, cols []string, rows [][]any) (int64, error)

	// TranslateError converts a driver error into a dao sentinel and, for
	// constraint violations, a *ConstraintError carrying the constraint name. It
	// returns the input unchanged when the error is unrecognized.
	TranslateError(err error) error

	// TwoPhaseSupported reports whether the dialect supports prepared transactions
	// (PREPARE TRANSACTION / COMMIT PREPARED) for opt-in true two-phase commit
	// (ADR-0005 §2.3). Default false.
	TwoPhaseSupported() bool

	// Prepare is phase one of a two-phase commit: it prepares the open driver
	// transaction under the global id gid, dissociating it from the session and
	// making it durable pending a CommitPrepared/RollbackPrepared decision. After
	// a successful Prepare the TxConn must no longer be used. Implemented only
	// when TwoPhaseSupported reports true (ADR-0005 §2.3).
	Prepare(ctx context.Context, tx TxConn, gid string) error

	// CommitPrepared is phase two of a two-phase commit: it durably commits the
	// transaction previously prepared under gid. It executes on the pool
	// connection — the preparing session is gone by design.
	CommitPrepared(ctx context.Context, conn DataConn, gid string) error

	// RollbackPrepared aborts the transaction previously prepared under gid,
	// releasing its locks. Used when another participant fails phase one.
	RollbackPrepared(ctx context.Context, conn DataConn, gid string) error

	// SupportsTransactions reports whether the dialect supports interactive
	// transactions (DataConn.Begin). OLAP / append-only stores (e.g. BigQuery)
	// report false; the transaction layer then returns ErrUnsupported on first
	// touch of such a connection (ADR-0008 §2.3). Default true.
	SupportsTransactions() bool

	// SupportsUpsert reports whether the dialect can express an INSERT-with-conflict
	// upsert via BuildUpsertSuffix. Stores with no ON CONFLICT / ON DUPLICATE KEY
	// (e.g. BigQuery — MERGE-only) report false; DAO.Upsert and batch conflict
	// handling then return ErrUnsupported (ADR-0008 §2.4). Default true.
	SupportsUpsert() bool

	// SupportsLastInsertID reports whether a non-RETURNING INSERT can return a
	// server-generated id via Result.LastInsertId (the MySQL model). GenericDialect
	// is RETURNING-based and reports false; when a dialect supports neither
	// RETURNING nor LastInsertID, DAO.Insert performs the DML and returns the zero
	// ID with a nil error — a documented no-generated-id insert (ADR-0008 §2.6).
	SupportsLastInsertID() bool
}
