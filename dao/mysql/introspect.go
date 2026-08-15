package mysql

import (
	"context"

	"github.com/yongjohnlee80/golib/dao"
)

// Schema introspection (golib-dao ADR-0013 §3.3) over information_schema.
// An empty schema resolves to the connection's current database via
// DATABASE(). String flags (IS_NULLABLE, COLUMN_KEY) are compared in Go, not
// SQL, to avoid driver bool-conversion trouble.

// MysqlDialect opts into the qualified-table and introspection capabilities
// (ADR-0013).
var (
	_ dao.TableQuoter  = MysqlDialect{}
	_ dao.Introspector = MysqlDialect{}
)

// ListSchemas lists user databases, excluding the four system schemas.
func (MysqlDialect) ListSchemas(ctx context.Context, q dao.Querier) ([]dao.SchemaInfo, error) {
	const stmt = `SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('mysql', 'sys', 'performance_schema', 'information_schema')
		ORDER BY SCHEMA_NAME`
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

// ListTables lists the tables and views of schema. An empty schema means the
// connection's current database.
func (MysqlDialect) ListTables(ctx context.Context, q dao.Querier, schema string) ([]dao.TableInfo, error) {
	const stmt = `SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE())
		ORDER BY TABLE_NAME`
	rows, err := q.QueryContext(ctx, stmt, schema)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []dao.TableInfo
	for rows.Next() {
		var t dao.TableInfo
		var typ string
		if err := rows.Scan(&t.Schema, &t.Name, &typ); err != nil {
			return nil, err
		}
		if typ == "VIEW" || typ == "SYSTEM VIEW" {
			t.Kind = dao.TableKindView
		} else {
			t.Kind = dao.TableKindTable
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListColumns lists the columns of schema.table in ordinal order. An empty
// schema means the connection's current database.
func (MysqlDialect) ListColumns(ctx context.Context, q dao.Querier, schema, table string) ([]dao.ColumnInfo, error) {
	const stmt = `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT,
			ORDINAL_POSITION, COLUMN_KEY
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE()) AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`
	rows, err := q.QueryContext(ctx, stmt, schema, table)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []dao.ColumnInfo
	for rows.Next() {
		var c dao.ColumnInfo
		var nullable, key string
		var def *string
		if err := rows.Scan(&c.Name, &c.DataType, &nullable, &def, &c.Position, &key); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		if def != nil {
			c.Default, c.HasDefault = *def, true
		}
		c.PrimaryKey = key == "PRI"
		out = append(out, c)
	}
	return out, rows.Err()
}
