package dao

// Dialect captures the SQL differences between databases. There is one Dialect
// per driver, and it is the stable base contract: adding a database means
// implementing [DataConn] and Dialect once, with no change to the engine, the
// interfaces, or any entity declaration.
//
// Beyond the base contract, a dialect may opt into optional capabilities the
// engine and package-level helpers probe by type assertion — [TableQuoter]
// (schema-qualified table quoting) and [Introspector] (catalog listing), plus
// the row-stream extension [RowsColumns] on a driver's Rows. Capabilities are
// deliberately not Dialect methods and have no GenericDialect defaults
// (ADR-0013 rev 1; KB convention interface-evolution-capability-interfaces).
type Dialect interface {
	// Name is a short dialect id ("postgres", "mysql", "sqlite").
	Name() string

	// Placeholder renders the nth (1-based) bind placeholder: "$1" (Postgres) or
	// "?" (MySQL/SQLite).
	Placeholder(n int) string

	// MaxBindParams is the maximum number of bind parameters per statement. It
	// drives batch chunking. Postgres: 65535. SQLite: 999 (or 32766 since 3.32).
	// MySQL: effectively packet-bound — use a conservative cap. MSSQL: 2100.
	MaxBindParams() int

	// MaxBatchRows optionally caps rows per batch statement regardless of the
	// parameter count (0 means no extra cap), bounding statement size for planner
	// sanity.
	MaxBatchRows() int

	// QuoteIdent quotes a table or column identifier for the dialect.
	QuoteIdent(ident string) string

	// TranslateError converts a driver error into a dao sentinel and, for
	// constraint violations, a *ConstraintError carrying the constraint name. It
	// returns the input unchanged when the error is unrecognized.
	TranslateError(err error) error
}

// TableQuoter is an optional [Dialect] capability (ADR-0013 §2): a dialect
// that understands schema-qualified table names implements it, and the engine
// then quotes table-position identifiers through QuoteTable instead of
// QuoteIdent. Deliberately NOT part of Dialect and NOT implemented by
// [GenericDialect]: an embedded promoted default would silently override the
// table quoting of existing dialects with their own QuoteIdent conventions
// (the BigQuery backtick dot-path) in mixed-version builds. A dialect that
// does not implement it keeps today's behavior — the whole table string is
// quoted as one identifier via QuoteIdent.
type TableQuoter interface {
	// QuoteTable quotes an identifier appearing in table position:
	// "app.users" renders as "app"."users" (each dot-separated part quoted
	// separately). Table identifiers containing a literal dot in the name
	// itself are not supported in qualified form — the dot is the
	// qualification separator.
	QuoteTable(ident string) string
}
