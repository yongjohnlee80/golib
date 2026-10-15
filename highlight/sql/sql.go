// Package sql is SQL's highlighting at its first import path: PostgreSQL, SQLite and MySQL, each as
// its own definition.
//
// Deprecated: SQL's source-language module is parse/sql, as every other language's is
// parse/<lang>: use parse/sql's Definitions, Highlighter and Dialect. This package forwards to it,
// unchanged in what it returns, until a later release removes it.
package sql

import (
	"github.com/yongjohnlee80/golib/highlight"
	psql "github.com/yongjohnlee80/golib/parse/sql"
)

// Dialect is which SQL a highlighter reads.
//
// Deprecated: use parse/sql's Dialect.
type Dialect = psql.Dialect

// The dialects.
//
// Deprecated: use parse/sql's PostgreSQLDialect, SQLiteDialect and MySQLDialect.
const (
	PostgreSQL = psql.PostgreSQLDialect
	SQLite     = psql.SQLiteDialect
	MySQL      = psql.MySQLDialect
)

// Definitions are the three dialects as syntax definitions, in the shape this package has always
// returned: each carries its Highlighter, shared by every document that uses it.
//
// Deprecated: use parse/sql's Definitions, whose sources give each document a highlighter of its
// own.
func Definitions() []highlight.Definition {
	defs := psql.Definitions()
	for i := range defs {
		defs[i].Highlighter = defs[i].NewSource(nil).Highlighter
		defs[i].SourceFactory = nil
	}
	return defs
}

// Highlighter returns the highlighter for d.
//
// Deprecated: use parse/sql's Highlighter.
func Highlighter(d Dialect) highlight.Highlighter { return psql.Highlighter(d) }
