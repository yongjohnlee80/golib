package mysql

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers the "mysql" database/sql driver
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

// Open opens a MySQL dao.DataConn named "mysql". dsn is a go-sql-driver DSN
// ("user:pass@tcp(host:3306)/db?parseTime=true"); parseTime=true is
// recommended so DATE/DATETIME columns scan into time.Time.
func Open(ctx context.Context, dsn string, opts ...Option) (dao.DataConn, error) {
	return OpenNamed(ctx, "mysql", dsn, opts...)
}

// OpenNamed opens a dao.DataConn with an explicit name (used for transaction
// context keying and logs).
func OpenNamed(ctx context.Context, name, dsn string, opts ...Option) (dao.DataConn, error) {
	db, err := sql.Open("mysql", dsn)
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
	return &mysqlConn{db: db, name: name}, nil
}

// mysqlConn is a dao.DataConn over a *sql.DB. The stdlib *sql.Rows and
// sql.Result already satisfy dao.Rows / dao.Result (and *sql.Rows satisfies
// dao.RowsColumns) by value, so only the method return types need adapting.
type mysqlConn struct {
	db   *sql.DB
	name string
}

func (c *mysqlConn) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *mysqlConn) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	res, err := c.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (c *mysqlConn) Dialect() dao.Dialect { return MysqlDialect{} }

func (c *mysqlConn) Begin(ctx context.Context) (dao.TxConn, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &mysqlTx{tx: tx}, nil
}

func (c *mysqlConn) Name() string { return c.name }
func (c *mysqlConn) Close() error { return c.db.Close() }

// mysqlTx is a dao.TxConn over a *sql.Tx.
type mysqlTx struct {
	tx *sql.Tx
}

func (t *mysqlTx) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (t *mysqlTx) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	res, err := t.tx.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *mysqlTx) Commit() error   { return t.tx.Commit() }
func (t *mysqlTx) Rollback() error { return t.tx.Rollback() }
