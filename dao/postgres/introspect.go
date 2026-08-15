package postgres

import (
	"context"

	"github.com/yongjohnlee80/golib/dao"
)

// Schema introspection (golib-dao ADR-0013 §3.3). Queries read pg_catalog
// rather than information_schema: it is faster, complete (partitioned tables,
// materialized views), and keys the column lookup on a bind-safe
// format('%I.%I', $1, $2)::regclass instead of interpolated identifiers.

// SupportsIntrospection reports true: the dialect implements the catalog
// listing trio over pg_catalog.
func (PostgresDialect) SupportsIntrospection() bool { return true }

// ListSchemas lists user-visible schemas, excluding pg_catalog's own
// namespaces and information_schema.
func (PostgresDialect) ListSchemas(ctx context.Context, q dao.Querier) ([]dao.SchemaInfo, error) {
	const stmt = `SELECT nspname FROM pg_catalog.pg_namespace
		WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
		ORDER BY nspname`
	rows, err := q.QueryContext(ctx, stmt)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []dao.SchemaInfo
	for rows.Next() {
		var s dao.SchemaInfo
		if err := rows.Scan(&s.Name); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListTables lists the tables (relkind r/p) and views (v/m) of schema.
// An empty schema means "public".
func (PostgresDialect) ListTables(ctx context.Context, q dao.Querier, schema string) ([]dao.TableInfo, error) {
	if schema == "" {
		schema = "public"
	}
	const stmt = `SELECT n.nspname, c.relname, c.relkind::text
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p', 'v', 'm')
		ORDER BY c.relname`
	rows, err := q.QueryContext(ctx, stmt, schema)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []dao.TableInfo
	for rows.Next() {
		var t dao.TableInfo
		var kind string
		if err := rows.Scan(&t.Schema, &t.Name, &kind); err != nil {
			return nil, err
		}
		switch kind {
		case "v", "m":
			t.Kind = dao.TableKindView
		default:
			t.Kind = dao.TableKindTable
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListColumns lists the columns of schema.table in attnum order, with
// format_type text, nullability, default expression, and primary-key
// membership. An empty schema means "public".
func (PostgresDialect) ListColumns(ctx context.Context, q dao.Querier, schema, table string) ([]dao.ColumnInfo, error) {
	if schema == "" {
		schema = "public"
	}
	const stmt = `SELECT a.attname,
			pg_catalog.format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull,
			pg_catalog.pg_get_expr(ad.adbin, ad.adrelid),
			a.attnum::int,
			COALESCE(i.indisprimary, false)
		FROM pg_catalog.pg_attribute a
		LEFT JOIN pg_catalog.pg_attrdef ad
			ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		LEFT JOIN pg_catalog.pg_index i
			ON i.indrelid = a.attrelid AND i.indisprimary AND a.attnum = ANY(i.indkey)
		WHERE a.attrelid = format('%I.%I', $1::text, $2::text)::regclass
			AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`
	rows, err := q.QueryContext(ctx, stmt, schema, table)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []dao.ColumnInfo
	for rows.Next() {
		var c dao.ColumnInfo
		var def *string
		if err := rows.Scan(&c.Name, &c.DataType, &c.Nullable, &def, &c.Position, &c.PrimaryKey); err != nil {
			return nil, err
		}
		if def != nil {
			c.Default, c.HasDefault = *def, true
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
