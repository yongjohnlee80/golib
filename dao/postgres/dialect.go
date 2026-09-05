package postgres

import (
	"context"
	"fmt"
	"strings"

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

// PostgresDialect opts into the qualified-table and introspection
// capabilities (ADR-0013).
var (
	_ dao.TableQuoter  = PostgresDialect{}
	_ dao.Introspector = PostgresDialect{}
)

// Name returns "postgres".
func (PostgresDialect) Name() string { return dao.DialectPostgres }

// QuoteTable implements dao.TableQuoter: each dot-separated qualification
// part is double-quoted separately, so "app.users" renders "app"."users"
// (ADR-0013 §2). Unqualified names render identically to QuoteIdent.
func (d PostgresDialect) QuoteTable(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = d.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

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

// --- two-phase commit (ADR-0005 §2.3) ----------------------------------------

// TwoPhaseSupported reports true: Postgres implements prepared transactions.
// The SERVER must also allow them — max_prepared_transactions defaults to 0
// (disabled); dao.Transaction surfaces the resulting error at Commit.
func (PostgresDialect) TwoPhaseSupported() bool { return true }

// Prepare issues PREPARE TRANSACTION for the open driver transaction, making it
// durable and dissociating it from the session, then releases the session's
// connection back to the pool. After a successful Prepare the transaction can
// only be finished with CommitPrepared / RollbackPrepared.
// The tx must be a transaction produced by this package: it carries the
// command-tag verification that detects Postgres silently rolling back a
// PREPARE TRANSACTION issued in an aborted transaction.
func (PostgresDialect) Prepare(ctx context.Context, tx dao.TxConn, gid string) error {
	p, ok := tx.(interface {
		prepareTx(ctx context.Context, gid string) error
	})
	if !ok {
		return fmt.Errorf("postgres: executor %T does not support PREPARE TRANSACTION (not a postgres transaction)", tx)
	}
	return p.prepareTx(ctx, gid)
}

// CommitPrepared durably commits the transaction prepared under gid. It runs on
// the pool: any connection may finish a prepared transaction.
func (PostgresDialect) CommitPrepared(ctx context.Context, conn dao.DataConn, gid string) error {
	if _, err := conn.ExecContext(ctx, "COMMIT PREPARED "+quoteLiteral(gid)); err != nil {
		return translateError(err)
	}
	return nil
}

// RollbackPrepared aborts the transaction prepared under gid, releasing its locks.
func (PostgresDialect) RollbackPrepared(ctx context.Context, conn dao.DataConn, gid string) error {
	if _, err := conn.ExecContext(ctx, "ROLLBACK PREPARED "+quoteLiteral(gid)); err != nil {
		return translateError(err)
	}
	return nil
}

// quoteLiteral renders s as a single-quoted SQL string literal. PREPARE
// TRANSACTION and friends are utility statements that cannot take bind
// parameters, so the gid must be inlined; doubling embedded quotes keeps the
// literal safe.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
