package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/yongjohnlee80/golib/dao"
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
	return OpenNamed(ctx, "sqlite", dsn, opts...)
}

// OpenNamed opens a dao.DataConn with an explicit name (used for transaction
// context keying and logs).
func OpenNamed(ctx context.Context, name, dsn string, opts ...Option) (dao.DataConn, error) {
	db, err := sql.Open("sqlite", dsn)
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
