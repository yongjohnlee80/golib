package postgres

import "github.com/yongjohnlee80/golib/dao"

// PostgreSQL declares its optional capabilities EXPLICITLY, including the two
// it used to inherit from dao.GenericDialect.
//
// Inheriting a capability is how a dialect loses one silently: the day the
// generic default is removed, an engine that never declared the behaviour
// stops having it, and the build stays green. Declaring it here means the
// capability belongs to this dialect and cannot be taken away by a change to
// another type.

// ReturningClause implements dao.Returner: PostgreSQL returns the generated id
// from the INSERT itself, so no second round trip is needed.
func (PostgresDialect) ReturningClause(quotedIDCol string) string {
	return dao.StandardReturningClause(quotedIDCol)
}

// BuildUpsertSuffix implements dao.Upserter with the standard ON CONFLICT
// form. The dialect is passed so its own QuoteIdent does the quoting.
func (d PostgresDialect) BuildUpsertSuffix(conflictCols, updateCols []string) string {
	return dao.StandardUpsertSuffix(d, conflictCols, updateCols)
}
