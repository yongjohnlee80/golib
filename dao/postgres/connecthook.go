package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yongjohnlee80/golib/dao"
)

// WithConnectHook runs fn on every physical connection this pool opens,
// including replacements, before that connection serves any query. A non-nil
// error from fn FAILS the connect: pgx closes the connection and it never
// enters the pool, so a session that could not be set up never serves.
//
// Registration is an Open option and there is no setter, deliberately: a hook
// installed after the pool exists would miss the connections already in it,
// which is precisely the gap it is here to close.
//
// This is pgx's own AfterConnect seam. The hook receives a dao-level
// [dao.ConnectedConn], never *pgx.Conn, so one hook body is portable across
// drivers and a consumer does not take a pgx dependency to write one.
func WithConnectHook(fn dao.ConnectHook) Option {
	return func(c *pgxpool.Config) {
		if fn == nil {
			return
		}
		// COMPOSE, never overwrite. c.AfterConnect may already carry a
		// callback — from an earlier WithConnectHook, or from a caller's own
		// Option reaching pgxpool.Config directly — and assigning over it
		// would drop that setup silently on every connection. The prior
		// callback runs FIRST and its error SHORT-CIRCUITS: if the connection
		// could not be set up by the earlier hook, the later one must not run
		// against a half-configured session.
		prior := c.AfterConnect
		c.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if prior != nil {
				if err := prior(ctx, conn); err != nil {
					return err
				}
			}
			return fn(ctx, pgxConnectedConn{conn: conn})
		}
	}
}

// pgxConnectedConn adapts a single *pgx.Conn to [dao.ConnectedConn]. It reuses
// the package's existing pgxRows and pgxResult adapters rather than adding a
// second translation of the same types.
type pgxConnectedConn struct{ conn *pgx.Conn }

func (c pgxConnectedConn) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return &pgxRows{rows: rows}, nil
}

func (c pgxConnectedConn) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	tag, err := c.conn.Exec(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgxResult{tag: tag}, nil
}
