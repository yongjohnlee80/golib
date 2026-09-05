package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/dao/internal/stdsql"
)

// Option configures the underlying *sql.DB at Open time.
type Option func(*sql.DB)

// MaxOpenConns caps the number of open connections.
func MaxOpenConns(n int) Option { return func(db *sql.DB) { db.SetMaxOpenConns(n) } }

// MaxIdleConns caps the number of idle connections.
func MaxIdleConns(n int) Option { return func(db *sql.DB) { db.SetMaxIdleConns(n) } }

// ConnMaxLifetime caps how long a connection may be reused.
func ConnMaxLifetime(d time.Duration) Option { return func(db *sql.DB) { db.SetConnMaxLifetime(d) } }

// ConnMaxIdleTime caps how long a connection may sit idle.
func ConnMaxIdleTime(d time.Duration) Option { return func(db *sql.DB) { db.SetConnMaxIdleTime(d) } }

// Open opens a SQLite dao.DataConn named "sqlite". dsn is a modernc.org/sqlite
// DSN — a file path, or ":memory:" (pair an in-memory DSN with MaxOpenConns(1) so
// every query hits the same database).
func Open(ctx context.Context, dsn string, opts ...Option) (dao.DataConn, error) {
	return OpenNamed(ctx, dao.DialectSQLite, dsn, opts...)
}

// OpenNamed opens a dao.DataConn with an explicit name (used for transaction
// context keying and logs).
func OpenNamed(ctx context.Context, name, dsn string, opts ...Option) (dao.DataConn, error) {
	return OpenHooked(ctx, name, dsn, nil, opts...)
}

// OpenHooked opens a dao.DataConn that runs hook once on every PHYSICAL
// connection the pool makes — the first one, and every reconnect after an
// idle timeout, a lifetime expiry or a dropped connection. It does not run
// when the pool hands out a connection it already has open. Use it for
// session state that must be true of every connection and cannot be set per
// statement: a session variable, a role, a SQLite PRAGMA.
//
// A nil hook opens the database the ordinary way, which is what OpenNamed
// does, so there is one open path here rather than two.
//
// The hook is NOT an Option. An Option is func(*sql.DB) and runs after the
// pool exists; a connect hook has to be installed before it, because it is
// part of how a connection is made. Keeping Option's type unchanged is also
// what keeps this addition from breaking callers who convert functions to it.
//
// IF THE HOOK RETURNS AN ERROR the connection is closed and the error is
// returned to whoever triggered the connect. One exception, and it is worth
// knowing before you write a hook: an error that is or wraps
// driver.ErrBadConn asks database/sql to RETRY, so the hook runs again —
// three times in total, measured — before the error surfaces. Return
// driver.ErrBadConn only when you mean "this connection is unusable, get
// another one"; for a hook that has simply failed, any other error stops
// the connect immediately.
func OpenHooked(ctx context.Context, name, dsn string, hook dao.ConnectHook,
	opts ...Option,
) (dao.DataConn, error) {
	db, err := stdsql.OpenHooked("sqlite", dsn, hook)
	if err != nil {
		return nil, err
	}
	for _, o := range opts {
		o(db)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteConn{db: db, name: name}, nil
}

// sqliteConn is a dao.DataConn over a *sql.DB. The stdlib *sql.Rows and sql.Result
// already satisfy dao.Rows / dao.Result by value, so only the method return types
// need adapting.
type sqliteConn struct {
	db   *sql.DB
	name string
}

func (c *sqliteConn) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *sqliteConn) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	res, err := c.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (c *sqliteConn) Dialect() dao.Dialect { return SqliteDialect{} }

func (c *sqliteConn) Begin(ctx context.Context) (dao.TxConn, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx}, nil
}

func (c *sqliteConn) Name() string { return c.name }
func (c *sqliteConn) Close() error { return c.db.Close() }

// sqliteTx is a dao.TxConn over a *sql.Tx.
type sqliteTx struct {
	tx *sql.Tx
}

func (t *sqliteTx) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (t *sqliteTx) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	res, err := t.tx.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *sqliteTx) Commit() error   { return t.tx.Commit() }
func (t *sqliteTx) Rollback() error { return t.tx.Rollback() }
