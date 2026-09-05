package sqlite

import (
	"strings"

	"github.com/yongjohnlee80/golib/dao"
)

// SqliteDialect implements dao.Dialect for SQLite. It embeds dao.GenericDialect
// (double-quoted identifiers, RETURNING, Postgres-style ON CONFLICT — all valid
// on modern SQLite) and overrides only the SQLite-specific bits: "?" positional
// placeholders, the 999 bind-parameter limit, and result-code error translation.
// There is no COPY fast-path (CopySupported stays false), so batches use the
// chunked multi-row INSERT path.
type SqliteDialect struct {
	dao.GenericDialect
}

// SqliteDialect opts into the qualified-table and introspection capabilities
// (ADR-0013).
var (
	_ dao.TableQuoter  = SqliteDialect{}
	_ dao.Introspector = SqliteDialect{}
)

// Name returns "sqlite".
func (SqliteDialect) Name() string { return dao.DialectSQLite }

// QuoteTable implements dao.TableQuoter: each dot-separated qualification
// part ("main.users", attached-database names) is double-quoted separately
// (ADR-0013 §2). Unqualified names render identically to QuoteIdent.
func (d SqliteDialect) QuoteTable(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = d.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

// Placeholder renders "?" — SQLite uses positional placeholders.
func (SqliteDialect) Placeholder(int) string { return "?" }

// MaxBindParams returns SQLite's conservative default ceiling, 999.
func (SqliteDialect) MaxBindParams() int { return 999 }

// TranslateError maps SQLite constraint result codes to dao sentinels.
func (SqliteDialect) TranslateError(err error) error { return translateError(err) }
