package postgres

import (
	"context"
	"fmt"

	"github.com/yongjohnlee80/golib/dao"
)

// PostgresDialect implements dao.Dialect for PostgreSQL. It embeds
// dao.GenericDialect (whose conventions are already Postgres-shaped: $n
// placeholders, 65535 bind params, double-quoted identifiers, RETURNING, and
// Postgres-style ON CONFLICT) and overrides only the parts that are genuinely
// Postgres-specific: COPY support and SQLSTATE error translation.
type PostgresDialect struct {
	dao.GenericDialect
}

// Name returns "postgres".
func (PostgresDialect) Name() string { return "postgres" }

// CopySupported reports true: the driver uses pgx CopyFrom for the bulk-load
// fast-path.
func (PostgresDialect) CopySupported() bool { return true }

// Copy bulk-loads rows into table(cols) using the executor's pgx CopyFrom. The
// executor is the dao.DataConn/TxConn the batch is running on; it must be a
// connection produced by this package.
func (PostgresDialect) Copy(ctx context.Context, ex any, table string, cols []string, rows [][]any) (int64, error) {
	c, ok := ex.(pgxCopier)
	if !ok {
		return 0, fmt.Errorf("postgres: executor %T does not support COPY (not a postgres connection)", ex)
	}
	return c.copyRows(ctx, table, cols, rows)
}

// TranslateError maps Postgres SQLSTATE codes to dao sentinels / *ConstraintError.
func (PostgresDialect) TranslateError(err error) error { return translateError(err) }

// pgxCopier is implemented by this package's connection/transaction wrappers to
// expose pgx's native CopyFrom to the dialect.
type pgxCopier interface {
	copyRows(ctx context.Context, table string, cols []string, rows [][]any) (int64, error)
}
