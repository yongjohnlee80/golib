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

// Qualified-table quoting and schema introspection are OPTIONAL dialect
// capabilities: a dialect provides one by implementing its interface, and the
// engine discovers that with a type assertion rather than by asking. These
// declarations are what make the capability real for SQLite — delete one and
// the engine silently falls back to the generic behaviour, which still
// compiles.
// REFERENCE: dao/sqlite/introspect_test.go
var (
	_ dao.TableQuoter  = SqliteDialect{}
	_ dao.Introspector = SqliteDialect{}
)

// Name returns "sqlite".
func (SqliteDialect) Name() string { return dao.DialectSQLite }

// QuoteTable quotes a table name that may carry a database qualification,
// such as "main.users" or the name of an attached database.
//
// Each dot-separated part is quoted SEPARATELY. Quoting the whole string at
// once would produce "main.users" as a single identifier — one table whose
// name contains a dot — which is a different table from the one the caller
// asked for, and usually one that does not exist. A name with no dot comes out
// exactly as QuoteIdent would render it.
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
