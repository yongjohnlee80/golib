package dao

// Canonical dialect names. These are the single source of truth for the string
// a [Dialect.Name] returns, and for the default connection name each driver's
// Open uses. Consumers compare against these constants instead of writing the
// string again: autodb needs to know "is this postgres?" and had begun
// declaring its own parallel set, which is one namespace too many for one
// concept (Johno, 2026-09-06).
//
// They are UNTYPED string constants on purpose. [Dialect] declares
// `Name() string`, so giving these a named type would force either a breaking
// change to that signature or a conversion at every return — and the point of
// this file is to remove duplication, not to add ceremony.
//
// The VALUES are exactly what each dialect already returned, so this is a
// de-duplication and not a rename: nothing persisted, logged or compared
// changes meaning.
//
// NOT IN THIS SET, deliberately: the `database/sql` driver-registration names
// passed to sql.Open by dao/mysql and dao/sqlite. Those belong to the
// third-party driver packages (go-sql-driver/mysql and modernc.org/sqlite
// register themselves under their own strings), and that they happen to equal
// two of the names below is a coincidence rather than a contract. Collapsing
// them would recreate, one level down, exactly the accidental agreement this
// file exists to remove.
const (
	// DialectPostgres is the name reported by dao/postgres.
	DialectPostgres = "postgres"
	// DialectMySQL is the name reported by dao/mysql.
	DialectMySQL = "mysql"
	// DialectSQLite is the name reported by dao/sqlite.
	DialectSQLite = "sqlite"
	// DialectBigQuery is the name reported by dao/bigquery.
	DialectBigQuery = "bigquery"

	// DialectGeneric is reported by [GenericDialect], the embeddable default
	// profile. It is a dialect name but NOT AN ENGINE: no database answers to
	// it, and it is what a driver reports if it forgets to override Name. It is
	// declared here so the set is complete and excluded from [EngineDialects]
	// so nothing iterates it as though it were a database.
	DialectGeneric = "generic"
)

// EngineDialects lists the names that identify a real database engine, in a
// stable order. DialectGeneric is excluded: see its doc comment.
//
// This is the one place a new engine is added, and dao/dialects_test.go
// asserts the list against the dialects actually implemented in the tree, in
// both directions.
func EngineDialects() []string {
	return []string{DialectPostgres, DialectMySQL, DialectSQLite, DialectBigQuery}
}
