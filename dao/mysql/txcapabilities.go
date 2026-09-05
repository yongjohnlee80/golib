package mysql

import (
	"context"
	"database/sql"

	"github.com/yongjohnlee80/golib/dao"
)

// ADR-0017 capabilities for the MySQL driver. MySQL opts into dao.TxBeginner
// and nothing else, which is the honest half of the §2.2a matrix:
//
//   - Isolation is fully expressible through database/sql, READ UNCOMMITTED
//     included — MySQL implements it literally.
//   - READ ONLY is expressible; explicit READ WRITE is NOT. sql.TxOptions
//     carries a bool, and ReadOnly=false renders a plain START TRANSACTION,
//     which is a request for the server default rather than an override of it.
//     So it is refused rather than silently downgraded.
//   - DEFERRABLE is PostgreSQL-only and is refused.
//   - dao.ContextTxConn is NOT implemented. *sql.Tx has no context finalizers,
//     and its BeginTx context already owns the transaction's lifetime and
//     auto-rollback. A pre-check would not bound an in-flight COMMIT and a
//     goroutine wrapper would race a false completion report, so claiming the
//     capability would be a lie the caller cannot detect (ADR-0017 §2.2).
//
// Compile-time proof of the base contracts and the one capability
// (ADR-0017 criterion 1).
var (
	_ dao.DataConn   = (*mysqlConn)(nil)
	_ dao.TxConn     = (*mysqlTx)(nil)
	_ dao.TxBeginner = (*mysqlConn)(nil)
)

// BeginTx starts a transaction with opts, satisfying dao.TxBeginner. Options
// are validated and then checked against what database/sql can express; both
// refusals happen before BeginTx reaches the driver, so a refused option never
// becomes a weaker transaction than the caller asked for.
func (c *mysqlConn) BeginTx(ctx context.Context, opts dao.TxOptions) (dao.TxConn, error) {
	sqlOpts, err := mysqlTxOptions(opts)
	if err != nil {
		return nil, err
	}
	tx, err := c.db.BeginTx(ctx, sqlOpts)
	if err != nil {
		return nil, err
	}
	return &mysqlTx{tx: tx}, nil
}

// mysqlTxOptions validates dao options, refuses the ones MySQL cannot honor,
// and renders the rest as database/sql's.
func mysqlTxOptions(opts dao.TxOptions) (*sql.TxOptions, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if opts.Access == dao.TxReadWrite {
		return nil, &dao.ErrTxOptionUnsupported{
			Driver: dao.DialectMySQL,
			Option: "Access=" + opts.Access.String(),
		}
	}
	if opts.Deferrable != dao.TxDeferrableDefault {
		return nil, &dao.ErrTxOptionUnsupported{
			Driver: dao.DialectMySQL,
			Option: "Deferrable=" + opts.Deferrable.String(),
		}
	}
	out := &sql.TxOptions{ReadOnly: opts.Access == dao.TxReadOnly}
	switch opts.Isolation {
	case dao.TxReadUncommitted:
		out.Isolation = sql.LevelReadUncommitted
	case dao.TxReadCommitted:
		out.Isolation = sql.LevelReadCommitted
	case dao.TxRepeatableRead:
		out.Isolation = sql.LevelRepeatableRead
	case dao.TxSerializable:
		out.Isolation = sql.LevelSerializable
	default:
		out.Isolation = sql.LevelDefault
	}
	return out, nil
}
