package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yongjohnlee80/golib/dao"
)

// Option configures the pgx pool at Open time.
type Option func(*pgxpool.Config)

// MaxOpenConns caps the pool size.
func MaxOpenConns(n int) Option {
	return func(c *pgxpool.Config) { c.MaxConns = int32(n) }
}

// MaxIdleConns sets the warm-pool floor. pgx pools differently from database/sql:
// it has no separate max-idle, so this maps to MinConns (the number of
// connections kept open).
func MaxIdleConns(n int) Option {
	return func(c *pgxpool.Config) { c.MinConns = int32(n) }
}

// ConnMaxLifetime caps how long a connection may live before recycling.
func ConnMaxLifetime(d time.Duration) Option {
	return func(c *pgxpool.Config) { c.MaxConnLifetime = d }
}

// ConnMaxIdleTime caps how long an idle connection is kept before closing.
func ConnMaxIdleTime(d time.Duration) Option {
	return func(c *pgxpool.Config) { c.MaxConnIdleTime = d }
}

// Open opens a pooled Postgres dao.DataConn named "postgres".
func Open(ctx context.Context, dsn string, opts ...Option) (dao.DataConn, error) {
	return OpenNamed(ctx, dao.DialectPostgres, dsn, opts...)
}

// OpenNamed opens a dao.DataConn with an explicit name (e.g. "postgres-gold") so a
// transaction can hold one tx per database (ADR-0005). The name is what dao keys
// transaction contexts and logs on.
func OpenNamed(ctx context.Context, name, dsn string, opts ...Option) (dao.DataConn, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	for _, o := range opts {
		o(cfg)
	}
	// Every connection records the ParameterStatus set the server reports, so a
	// pinned connection can hand a protocol relay the server's own list (§3.3).
	installStatusCapture(cfg)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &pgxConn{pool: pool, name: name}, nil
}

// pgxConn is a dao.DataConn backed by a pgx pool.
type pgxConn struct {
	pool *pgxpool.Pool
	name string
}

func (c *pgxConn) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := c.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return &pgxRows{rows: rows}, nil
}

func (c *pgxConn) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	tag, err := c.pool.Exec(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgxResult{tag: tag}, nil
}

func (c *pgxConn) Dialect() dao.Dialect { return PostgresDialect{} }

func (c *pgxConn) Begin(ctx context.Context) (dao.TxConn, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxTx{tx: tx, ctx: ctx}, nil
}

func (c *pgxConn) Name() string { return c.name }

func (c *pgxConn) Close() error { c.pool.Close(); return nil }

func (c *pgxConn) copyRows(ctx context.Context, table string, cols []string, rows [][]any) (int64, error) {
	return c.pool.CopyFrom(ctx, tableIdentifier(table), cols, pgx.CopyFromRows(rows))
}

// pgxTx is a dao.TxConn backed by a pgx transaction. It additionally satisfies
// dao.ContextTxConn (ADR-0017 §2.2) — pgx finalizers take a context natively,
// so the capability is honest here in a way it cannot be over *sql.Tx.
type pgxTx struct {
	tx  pgx.Tx
	ctx context.Context

	// closed records that a finalizer has DISPATCHED. It is not set by a
	// pre-dispatch context refusal, which leaves the transaction open and
	// rollable with a fresh context (ADR-0017 §2.2a fault state 1). It carries
	// no lock: one transaction is single-goroutine (ADR-0015), the same
	// contract pgx's own tx.closed relies on.
	closed bool
}

func (t *pgxTx) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := t.tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return &pgxRows{rows: rows}, nil
}

func (t *pgxTx) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	tag, err := t.tx.Exec(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgxResult{tag: tag}, nil
}

// Commit and Rollback are the unchanged dao.TxConn finalizers: they reuse the
// context Begin was called with. They record the handle as closed so a later
// context finalizer reports dao.ErrTransactionClosed rather than reaching pgx
// twice; the errors they themselves return are unchanged.
func (t *pgxTx) Commit() error   { t.closed = true; return t.tx.Commit(t.ctx) }
func (t *pgxTx) Rollback() error { t.closed = true; return t.tx.Rollback(t.ctx) }

func (t *pgxTx) copyRows(ctx context.Context, table string, cols []string, rows [][]any) (int64, error) {
	return t.tx.CopyFrom(ctx, tableIdentifier(table), cols, pgx.CopyFromRows(rows))
}

// tableIdentifier parses a possibly schema-qualified table name into a pgx
// Identifier, one part per qualification level, matching the engine's
// QuoteTable dot-separator contract (ADR-0013 §2).
func tableIdentifier(table string) pgx.Identifier {
	return pgx.Identifier(strings.Split(table, "."))
}

// pgxRows adapts pgx.Rows to dao.Rows.
type pgxRows struct {
	rows pgx.Rows
}

func (r *pgxRows) Next() bool             { return r.rows.Next() }
func (r *pgxRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r *pgxRows) Err() error             { return r.rows.Err() }
func (r *pgxRows) Close() error           { r.rows.Close(); return r.rows.Err() }

// Columns reports the result set's column names from pgx's field
// descriptions, satisfying dao.RowsColumns (ADR-0012).
func (r *pgxRows) Columns() ([]string, error) {
	fds := r.rows.FieldDescriptions()
	out := make([]string, len(fds))
	for i, fd := range fds {
		out[i] = string(fd.Name)
	}
	return out, nil
}

// pgxResult adapts a pgconn.CommandTag to dao.Result. Postgres has no
// LastInsertId; use a RETURNING id instead (which the dao Insert path prefers).
type pgxResult struct {
	tag pgconn.CommandTag
}

func (r pgxResult) RowsAffected() (int64, error) { return r.tag.RowsAffected(), nil }
func (r pgxResult) LastInsertId() (int64, error) {
	return 0, errors.New("postgres: no LastInsertId; use a RETURNING id")
}

// prepareTx runs PREPARE TRANSACTION for gid on this transaction's session and
// verifies it actually prepared: Postgres treats PREPARE TRANSACTION in an
// ABORTED transaction like COMMIT — it silently rolls back and reports success
// with a ROLLBACK command tag. Trusting the error alone would let a poisoned
// participant "pass" phase one and break the all-or-nothing guarantee, so the
// command tag is checked. On success the session's connection is released back
// to the pool (the prepared transaction lives on server-side).
func (t *pgxTx) prepareTx(ctx context.Context, gid string) error {
	tag, err := t.tx.Exec(ctx, "PREPARE TRANSACTION "+quoteLiteral(gid))
	if err != nil {
		return translateError(err)
	}
	if tag.String() != "PREPARE TRANSACTION" {
		return fmt.Errorf("postgres: prepare of %q did not take effect (server returned %q; the transaction was aborted and has been rolled back)", gid, tag.String())
	}
	// The transaction is now dissociated from the session; Rollback only
	// returns the connection to the pool (harmless no-tx warning server-side).
	t.closed = true
	_ = t.tx.Rollback(t.ctx)
	return nil
}
