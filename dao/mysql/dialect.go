package mysql

import (
	"strings"

	"github.com/yongjohnlee80/golib/dao"
)

// MysqlDialect implements dao.Dialect for MySQL. It embeds dao.GenericDialect
// and overrides the MySQL specifics: "?" positional placeholders, backtick
// identifier quoting, the LastInsertId profile (no RETURNING; ids from the
// OK packet —), the ON DUPLICATE KEY UPDATE upsert suffix, and
// errno-based error translation. There is no COPY fast-path, so batches use
// the chunked multi-row INSERT path.
type MysqlDialect struct {
	dao.GenericDialect
}

// Name returns "mysql".
func (MysqlDialect) Name() string { return dao.DialectMySQL }

// Placeholder renders "?" — MySQL uses positional placeholders.
func (MysqlDialect) Placeholder(int) string { return "?" }

// MaxBindParams returns 65535 — the binary protocol's hard parameter cap
// (2-byte parameter count). Statement byte size stays packet-sane because
// batch chunking divides this by the column count.
func (MysqlDialect) MaxBindParams() int { return 65535 }

// QuoteIdent backtick-quotes ident, escaping embedded backticks.
func (MysqlDialect) QuoteIdent(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

// QuoteTable backtick-quotes a table-position identifier, quoting each
// dot-separated qualification part separately: "app.users" → `app`.`users`.
func (d MysqlDialect) QuoteTable(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = d.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

// SupportsReturning reports false: MySQL has no INSERT ... RETURNING; the
// engine falls back to Result.LastInsertId (SupportsLastInsertID).
func (MysqlDialect) SupportsReturning() bool { return false }

// SupportsLastInsertID reports true: the generated id arrives in the OK
// packet and database/sql exposes it via Result.LastInsertId.
func (MysqlDialect) SupportsLastInsertID() bool { return true }

// BuildUpsertSuffix renders MySQL's ON DUPLICATE KEY UPDATE clause.
// MySQL cannot name a conflict target — the clause fires on
// ANY unique-key conflict, so conflictCols select only the update shape:
//
//   - conflictCols + updateCols → one "col = VALUES(col)" assignment per
//     update column (the upsert).
//   - conflictCols only → the do-nothing idiom, a self-assignment of the
//     first conflict column.
//   - no conflictCols (skip-conflicts) → the engine passes the insert columns
//     as updateCols; self-assignment of the first one.
//   - both empty → unreachable through the engine; renders the bare (invalid)
//     clause so a direct misuse fails loudly at the server rather than
//     silently dropping conflict handling.
func (d MysqlDialect) BuildUpsertSuffix(conflictCols, updateCols []string) string {
	if len(conflictCols) > 0 && len(updateCols) > 0 {
		var sb strings.Builder
		sb.WriteString("ON DUPLICATE KEY UPDATE ")
		for i, c := range updateCols {
			if i > 0 {
				sb.WriteString(", ")
			}
			q := d.QuoteIdent(c)
			sb.WriteString(q)
			sb.WriteString(" = VALUES(")
			sb.WriteString(q)
			sb.WriteString(")")
		}
		return sb.String()
	}
	// Do-nothing shapes: self-assign one known column.
	var col string
	switch {
	case len(conflictCols) > 0:
		col = conflictCols[0]
	case len(updateCols) > 0:
		col = updateCols[0]
	default:
		return "ON DUPLICATE KEY UPDATE" // unreachable via the engine; loud on direct misuse
	}
	q := d.QuoteIdent(col)
	return "ON DUPLICATE KEY UPDATE " + q + " = " + q
}

// TranslateError maps MySQL error numbers to dao sentinels.
func (MysqlDialect) TranslateError(err error) error { return translateError(err) }
