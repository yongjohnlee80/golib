package sqlite

import "github.com/yongjohnlee80/golib/dao"

// SqliteDialect implements dao.Dialect for SQLite. It embeds dao.GenericDialect
// (double-quoted identifiers, RETURNING, Postgres-style ON CONFLICT — all valid
// on modern SQLite) and overrides only the SQLite-specific bits: "?" positional
// placeholders, the 999 bind-parameter limit, and result-code error translation.
// There is no COPY fast-path (CopySupported stays false), so batches use the
// chunked multi-row INSERT path.
type SqliteDialect struct {
	dao.GenericDialect
}

// Name returns "sqlite".
func (SqliteDialect) Name() string { return "sqlite" }

// Placeholder renders "?" — SQLite uses positional placeholders.
func (SqliteDialect) Placeholder(int) string { return "?" }

// MaxBindParams returns SQLite's conservative default ceiling, 999.
func (SqliteDialect) MaxBindParams() int { return 999 }

// TranslateError maps SQLite constraint result codes to dao sentinels.
func (SqliteDialect) TranslateError(err error) error { return translateError(err) }
