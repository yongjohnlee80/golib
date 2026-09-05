package sqlite

import "github.com/yongjohnlee80/golib/dao"

// SQLite declares its optional capabilities EXPLICITLY, including the two it
// used to inherit from dao.GenericDialect. See the note in dao/postgres for
// why inheriting a capability is how an engine loses one silently.

// ReturningClause implements dao.Returner. SQLite has supported RETURNING
// since 3.35; the driver embedded here is well past that.
func (SqliteDialect) ReturningClause(quotedIDCol string) string {
	return dao.StandardReturningClause(quotedIDCol)
}

// BuildUpsertSuffix implements dao.Upserter with the standard ON CONFLICT
// form, quoted by this dialect's own QuoteIdent.
func (d SqliteDialect) BuildUpsertSuffix(conflictCols, updateCols []string) string {
	return dao.StandardUpsertSuffix(d, conflictCols, updateCols)
}
