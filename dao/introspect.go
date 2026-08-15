package dao

import "context"

// Schema introspection (ADR-0013): a uniform, dialect-owned catalog surface
// for listing schemas, tables, and columns. The package-level functions below
// are the consumer entry points; they delegate to the connection's [Dialect],
// which returns ErrUnsupported when it has no catalog queries
// (Dialect.SupportsIntrospection). Introspection statements run on the raw
// [Querier] (like Copy) and are not hook-observed.

// SchemaInfo describes one schema (namespace) in a database.
type SchemaInfo struct {
	// Name is the schema name (e.g. "public", "main", a MySQL database name).
	Name string
}

// TableKind classifies a relation returned by ListTables.
type TableKind string

const (
	// TableKindTable is an ordinary (or partitioned) table.
	TableKindTable TableKind = "table"
	// TableKindView is a view or materialized view.
	TableKindView TableKind = "view"
)

// TableInfo describes one table or view.
type TableInfo struct {
	// Schema is the containing schema's name.
	Schema string
	// Name is the relation name.
	Name string
	// Kind classifies the relation.
	Kind TableKind
}

// ColumnInfo describes one column of a table.
type ColumnInfo struct {
	// Name is the column name.
	Name string
	// DataType is the dialect-native type text (e.g. "integer",
	// "character varying(80)", "TEXT").
	DataType string
	// Nullable reports whether the column accepts NULL.
	Nullable bool
	// Default is the dialect-native default expression; meaningful only when
	// HasDefault is true.
	Default string
	// HasDefault reports whether the column declares a default.
	HasDefault bool
	// Position is the column's 1-based ordinal within the table.
	Position int
	// PrimaryKey reports whether the column is part of the primary key.
	PrimaryKey bool
}

// ListSchemas lists the database's schemas through conn's dialect. It returns
// ErrUnsupported (wrapped) when the dialect has no catalog queries.
func ListSchemas(ctx context.Context, conn DataConn) ([]SchemaInfo, error) {
	return conn.Dialect().ListSchemas(ctx, conn)
}

// ListTables lists the tables and views of schema through conn's dialect. An
// empty schema means the dialect's default (postgres: "public", mysql: the
// connection's current database, sqlite: "main").
func ListTables(ctx context.Context, conn DataConn, schema string) ([]TableInfo, error) {
	return conn.Dialect().ListTables(ctx, conn, schema)
}

// ListColumns lists the columns of schema.table through conn's dialect, in
// ordinal order. Empty schema semantics as in ListTables.
func ListColumns(ctx context.Context, conn DataConn, schema, table string) ([]ColumnInfo, error) {
	return conn.Dialect().ListColumns(ctx, conn, schema, table)
}
