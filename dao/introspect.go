package dao

import (
	"context"
	"fmt"
)

// Introspector is an optional [Dialect] capability (ADR-0013 §3): a dialect
// with catalog queries implements it, and the package-level ListSchemas /
// ListTables / ListColumns then work on its connections. Deliberately NOT
// part of Dialect and NOT implemented by [GenericDialect] — an embedded
// promoted default would silently claim the capability for every embedding
// dialect (the same hazard [TableQuoter] avoids).
type Introspector interface {
	// ListSchemas executes the dialect's catalog query for schemas
	// (namespaces) on q.
	ListSchemas(ctx context.Context, q Querier) ([]SchemaInfo, error)

	// ListTables lists the tables and views of schema. An empty schema means
	// the dialect's default (postgres: "public", mysql: the connection's
	// current database, sqlite: "main").
	ListTables(ctx context.Context, q Querier, schema string) ([]TableInfo, error)

	// ListColumns lists the columns of schema.table in ordinal order, with
	// type text, nullability, default expression, and primary-key membership.
	// Empty schema semantics as in ListTables.
	ListColumns(ctx context.Context, q Querier, schema, table string) ([]ColumnInfo, error)
}

// SupportsIntrospection reports whether d implements [Introspector].
func SupportsIntrospection(d Dialect) bool {
	_, ok := d.(Introspector)
	return ok
}

// Schema introspection (ADR-0013): a uniform, dialect-owned catalog surface
// for listing schemas, tables, and columns. The package-level functions below
// are the consumer entry points; they probe the connection's [Dialect] for
// the optional [Introspector] capability and return ErrUnsupported (wrapped)
// when it is absent. Introspection statements run on the raw [Querier] (like
// Copy) and are not hook-observed.

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
	// Nullable reports whether the column accepts NULL. Primary-key columns
	// are always reported non-nullable: dialects normalize engine quirks here
	// (SQLite's pragma reports notnull=0 for rowid-alias PKs, and its legacy
	// nullable-PK behavior for some non-INTEGER PKs is intentionally not
	// surfaced — ADR-0013 §3.1).
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
// ErrUnsupported (wrapped) when the dialect does not implement [Introspector].
func ListSchemas(ctx context.Context, conn DataConn) ([]SchemaInfo, error) {
	in, ok := conn.Dialect().(Introspector)
	if !ok {
		return nil, fmt.Errorf("%w: schema introspection", ErrUnsupported)
	}
	return in.ListSchemas(ctx, conn)
}

// ListTables lists the tables and views of schema through conn's dialect. An
// empty schema means the dialect's default (postgres: "public", mysql: the
// connection's current database, sqlite: "main"). It returns ErrUnsupported
// (wrapped) when the dialect does not implement [Introspector].
func ListTables(ctx context.Context, conn DataConn, schema string) ([]TableInfo, error) {
	in, ok := conn.Dialect().(Introspector)
	if !ok {
		return nil, fmt.Errorf("%w: schema introspection", ErrUnsupported)
	}
	return in.ListTables(ctx, conn, schema)
}

// ListColumns lists the columns of schema.table through conn's dialect, in
// ordinal order. Empty schema semantics as in ListTables. It returns
// ErrUnsupported (wrapped) when the dialect does not implement [Introspector].
func ListColumns(ctx context.Context, conn DataConn, schema, table string) ([]ColumnInfo, error) {
	in, ok := conn.Dialect().(Introspector)
	if !ok {
		return nil, fmt.Errorf("%w: schema introspection", ErrUnsupported)
	}
	return in.ListColumns(ctx, conn, schema, table)
}
