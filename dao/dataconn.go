package dao

import "context"

// Querier executes row-returning statements. It is satisfied by *sql.DB, *sql.Tx,
// and driver-native equivalents.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (Rows, error)
}

// Execer executes non-row statements.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (Result, error)
}

// Rows is the minimal row-stream surface the scanner needs — a subset of
// *sql.Rows so the core stays free of a database/sql import in its signatures.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// Result is the minimal exec-result surface — a subset of sql.Result.
type Result interface {
	RowsAffected() (int64, error)
	LastInsertId() (int64, error)
}

// DataConn is a database connection (pool) for one logical database instance.
// The DAL is written entirely against this interface; concrete drivers live in
// sub-packages (e.g. dao/postgres). One DataConn is one database; a Transaction
// may span several DataConns.
type DataConn interface {
	Querier
	Execer

	// Dialect returns the SQL dialect of this connection. It is stable for the
	// connection's lifetime.
	Dialect() Dialect

	// Begin starts a driver transaction, returning a tx-scoped executor.
	Begin(ctx context.Context) (TxConn, error)

	// Name identifies the connection for transaction-context keying and logs,
	// e.g. "postgres" or "postgres-gold".
	Name() string

	// Close releases the underlying pool.
	Close() error
}

// TxConn is a DataConn's transaction handle: an executor plus commit/rollback.
type TxConn interface {
	Querier
	Execer
	Commit() error
	Rollback() error
}
