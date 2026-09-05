// Package stdsql gives the database/sql-backed engines the per-connection
// hook that pgx hands the postgres engine for free.
//
// THE PROBLEM. pgx exposes AfterConnect, which is called with a *pgx.Conn —
// an object that can already run queries. database/sql's equivalent seam is
// driver.Connector.Connect, which hands back a raw driver.Conn. A driver.Conn
// cannot run a dao query: turning driver.Rows into dao.Rows means converting
// driver.Values into arbitrary Scan destinations, and that conversion is
// database/sql's own convert.go — 658 lines, one exported function. Rewriting
// it here to run a hook's SET or PRAGMA statement would be absurd, and every
// bug in the copy would be ours.
//
// THE APPROACH. Wrap the connector, and when a new physical connection is
// made, lend it to a throwaway *sql.DB for the duration of the hook. That
// gives the hook a *sql.Conn — the full, stdlib-converted surface — pinned to
// exactly the connection that was just opened. The lending is what needs care,
// and noCloseConn is the whole trick: the throwaway DB believes it owns the
// connection and closes it when we are done, while the real connection
// survives to be handed to the pool that asked for it.
package stdsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"

	"github.com/yongjohnlee80/golib/dao"
)

// OpenHooked opens a *sql.DB whose hook runs exactly once per PHYSICAL
// connection — on first open, and again on every reconnect the pool makes
// after an idle timeout, a lifetime expiry or a dropped connection. It does
// not run when the pool hands out an already-open connection.
//
// A nil hook is not an error; it opens the database the ordinary way, so
// callers do not need to branch.
//
// If the hook returns an error the connection is closed and the error is
// returned to whoever triggered the connect, which is how a hook that
// establishes required session state refuses to yield a half-configured
// connection.
func OpenHooked(driverName, dsn string, hook dao.ConnectHook) (*sql.DB, error) {
	if hook == nil {
		return sql.Open(driverName, dsn)
	}
	base, err := baseConnector(driverName, dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(&hookedConnector{base: base, hook: hook}), nil
}

// baseConnector gets a driver.Connector for a registered driver WITHOUT
// requiring the driver to export one.
//
// Drivers differ here and neither behaviour is wrong. go-sql-driver/mysql
// implements driver.DriverContext, so it parses the DSN once and returns its
// own connector. modernc.org/sqlite does not implement it and exports no
// connector at all, so the DSN is re-read on each connect, which is what
// sql.Open would have done anyway.
//
// The driver value itself comes from a probe *sql.DB, because database/sql's
// registry has no exported lookup. sql.Open does not dial, so the probe costs
// nothing and is closed immediately.
func baseConnector(driverName, dsn string) (driver.Connector, error) {
	probe, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	drv := probe.Driver()
	if err := probe.Close(); err != nil {
		return nil, fmt.Errorf("stdsql: closing driver probe for %q: %w", driverName, err)
	}
	if dc, ok := drv.(driver.DriverContext); ok {
		c, err := dc.OpenConnector(dsn)
		if err != nil {
			return nil, fmt.Errorf("stdsql: opening connector for %q: %w", driverName, err)
		}
		return c, nil
	}
	return dsnConnector{drv: drv, dsn: dsn}, nil
}

// dsnConnector is the fallback for a driver that does not implement
// driver.DriverContext: it re-opens from the DSN, exactly as sql.Open does.
type dsnConnector struct {
	drv driver.Driver
	dsn string
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.drv.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.drv }

type hookedConnector struct {
	base driver.Connector
	hook dao.ConnectHook
}

func (c *hookedConnector) Driver() driver.Driver { return c.base.Driver() }

func (c *hookedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if err := runHook(ctx, c.base.Driver(), conn, c.hook); err != nil {
		// The connection is unusable for the caller's purposes, and nothing
		// else holds it. Closing here is what keeps a failed hook from
		// leaking a connection per attempt.
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// runHook lends conn to a throwaway *sql.DB just long enough to give the hook
// a *sql.Conn, then takes it back intact.
func runHook(ctx context.Context, drv driver.Driver, conn driver.Conn, hook dao.ConnectHook) error {
	lender := &oneShot{drv: drv, conn: noCloseConn{Conn: conn}}

	tmp := sql.OpenDB(lender)
	// One connection, and no idle retention: the throwaway pool must never
	// try to open a second connection, and must not keep this one after the
	// hook returns.
	tmp.SetMaxOpenConns(1)
	tmp.SetMaxIdleConns(0)
	defer tmp.Close()

	sc, err := tmp.Conn(ctx)
	if err != nil {
		return fmt.Errorf("stdsql: lending the new connection to the hook: %w", err)
	}
	defer sc.Close()

	return hook(ctx, connectedConn{c: sc})
}

// errLendExhausted means the throwaway pool asked for a second connection.
// It cannot be satisfied: there is exactly one connection being lent, and
// opening another would run the hook against a connection nobody asked for.
var errLendExhausted = errors.New("stdsql: the hook's connection was already lent out")

// oneShot is a connector that yields one specific, already-open connection
// exactly once.
type oneShot struct {
	drv  driver.Driver
	conn driver.Conn
	once sync.Once
	used bool
}

func (o *oneShot) Driver() driver.Driver { return o.drv }

func (o *oneShot) Connect(context.Context) (driver.Conn, error) {
	var c driver.Conn
	o.once.Do(func() { c, o.used = o.conn, true })
	if c == nil {
		return nil, errLendExhausted
	}
	return c, nil
}

// noCloseConn is the connection the throwaway pool believes it owns. Every
// method is the real connection's; only Close is not, because the pool closes
// what it owns and this connection has somewhere else to be.
//
// Embedding rather than forwarding is deliberate: a driver.Conn's optional
// interfaces (QueryerContext, ExecerContext, ConnPrepareContext, and the rest)
// are not visible through an embedded interface, so database/sql falls back to
// the Prepare path that every driver.Conn is required to implement. For the
// statements a connect hook runs that is the right trade — correctness on
// every driver, rather than a forwarding table that silently rots as
// database/sql grows another optional interface.
type noCloseConn struct {
	driver.Conn
}

func (noCloseConn) Close() error { return nil }

// connectedConn presents the lent *sql.Conn as the dao-level surface a hook
// receives. *sql.Rows and sql.Result already satisfy dao.Rows and dao.Result,
// so only the return types need adapting.
type connectedConn struct{ c *sql.Conn }

func (h connectedConn) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	rows, err := h.c.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (h connectedConn) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	res, err := h.c.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

var _ dao.ConnectedConn = connectedConn{}
