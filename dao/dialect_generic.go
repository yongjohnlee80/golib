package dao

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// GenericDialect is a zero-dependency [Dialect] that follows Postgres/SQLite
// conventions: positional "$n" placeholders, the 65535 bind-parameter ceiling,
// double-quoted identifiers, RETURNING support, and Postgres-style ON CONFLICT
// upserts. It supports no COPY fast-path and passes driver errors through
// untranslated.
//
// It serves as the core's test dialect (so unit tests need no real database) and
// as an embeddable base for database/sql drivers that share these conventions; a
// driver overrides only the methods that differ (see dao/postgres).
//
// Per the dao design, the spelling here is exported (the ADR text wrote it
// lowercase) precisely so out-of-package drivers can embed it as a base.
type GenericDialect struct{}

// Name returns "generic".
func (GenericDialect) Name() string { return DialectGeneric }

// Placeholder renders "$n".
func (GenericDialect) Placeholder(n int) string { return "$" + strconv.Itoa(n) }

// MaxBindParams returns the Postgres ceiling, 65535.
func (GenericDialect) MaxBindParams() int { return 65535 }

// MaxBatchRows returns 0 (no extra per-statement row cap).
func (GenericDialect) MaxBatchRows() int { return 0 }

// QuoteIdent double-quotes ident, escaping embedded quotes.
//
// GenericDialect deliberately does NOT implement [TableQuoter] — a promoted
// QuoteTable would silently leak into every embedding dialect and override
// its own table-quoting conventions in mixed-version builds (ADR-0013 §2).
func (GenericDialect) QuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// SupportsReturning reports true.
func (GenericDialect) SupportsReturning() bool { return true }

// BuildUpsertSuffix renders a Postgres-style conflict clause. With no
// conflictCols it renders "ON CONFLICT DO NOTHING"; with conflictCols and no
// updateCols it also renders DO NOTHING; otherwise it renders
// "ON CONFLICT (<conflict>) DO UPDATE SET <col> = EXCLUDED.<col>, ...".
// Identifiers are quoted via QuoteIdent.
func (d GenericDialect) BuildUpsertSuffix(conflictCols, updateCols []string) string {
	if len(conflictCols) == 0 {
		return "ON CONFLICT DO NOTHING"
	}
	var sb strings.Builder
	sb.WriteString("ON CONFLICT (")
	for i, c := range conflictCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(d.QuoteIdent(c))
	}
	sb.WriteString(") DO ")
	if len(updateCols) == 0 {
		sb.WriteString("NOTHING")
		return sb.String()
	}
	sb.WriteString("UPDATE SET ")
	for i, c := range updateCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		q := d.QuoteIdent(c)
		sb.WriteString(q)
		sb.WriteString(" = EXCLUDED.")
		sb.WriteString(q)
	}
	return sb.String()
}

// CopySupported reports false: the generic dialect has no bulk-load fast-path.
func (GenericDialect) CopySupported() bool { return false }

// Copy always returns an error; GenericDialect does not support COPY.
func (GenericDialect) Copy(_ context.Context, _ any, _ string, _ []string, _ [][]any) (int64, error) {
	return 0, errors.New("dao: GenericDialect does not support COPY")
}

// TranslateError returns err unchanged.
func (GenericDialect) TranslateError(err error) error { return err }

// TwoPhaseSupported reports false: the generic dialect has no prepared-transaction
// support.
func (GenericDialect) TwoPhaseSupported() bool { return false }

// Prepare reports ErrTwoPhaseUnsupported: the generic dialect has no
// prepared-transaction support. Capable drivers (dao/postgres) override the
// two-phase trio together with TwoPhaseSupported.
func (GenericDialect) Prepare(context.Context, TxConn, string) error {
	return ErrTwoPhaseUnsupported
}

// CommitPrepared reports ErrTwoPhaseUnsupported (see Prepare).
func (GenericDialect) CommitPrepared(context.Context, DataConn, string) error {
	return ErrTwoPhaseUnsupported
}

// RollbackPrepared reports ErrTwoPhaseUnsupported (see Prepare).
func (GenericDialect) RollbackPrepared(context.Context, DataConn, string) error {
	return ErrTwoPhaseUnsupported
}

// SupportsTransactions reports true: the generic dialect is OLTP-shaped and
// supports interactive transactions (ADR-0008). A no-transaction driver (e.g.
// BigQuery) overrides this to false.
func (GenericDialect) SupportsTransactions() bool { return true }

// SupportsUpsert reports true: the generic dialect renders Postgres-style
// ON CONFLICT upserts (ADR-0008). A store with no INSERT-suffix upsert overrides
// this to false.
func (GenericDialect) SupportsUpsert() bool { return true }

// SupportsLastInsertID reports false: the generic dialect is RETURNING-based, so
// Insert never needs Result.LastInsertId. A LastInsertId-style driver (e.g. MySQL,
// SupportsReturning=false) overrides this to true (ADR-0008 §2.6).
//
// GenericDialect implements neither [TableQuoter] nor [Introspector] — both
// are opt-in capability interfaces a promoted default would silently inject
// into embedding dialects (ADR-0013).
func (GenericDialect) SupportsLastInsertID() bool { return false }
