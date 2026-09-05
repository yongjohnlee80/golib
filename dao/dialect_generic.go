package dao

import (
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

// TranslateError returns err unchanged.
func (GenericDialect) TranslateError(err error) error { return err }
