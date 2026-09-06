package mysql

import (
	"context"
	"strings"

	"github.com/yongjohnlee80/golib/dao"
)

// Schema introspection (golib-dao ADR-0013 §3.3) over information_schema.
// An empty schema resolves to the connection's current database via
// DATABASE(). String flags (IS_NULLABLE, COLUMN_KEY) are compared in Go, not
// SQL, to avoid driver bool-conversion trouble.

// MysqlDialect opts into the qualified-table and introspection capabilities.
var (
	_ dao.TableQuoter         = MysqlDialect{}
	_ dao.Introspector        = MysqlDialect{}
	_ dao.RoutineIntrospector = MysqlDialect{}
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

// ListRoutines lists the functions and procedures of schema:
// information_schema.ROUTINES joined to PARAMETERS ordered by
// ORDINAL_POSITION. Parameters render "MODE name type" comma-joined;
// position 0 (the return row, functions only) renders after "->" —
// "(args) -> result" for functions, "(args)" for procedures. MySQL has no
// overloads; ordering is schema, name.
func (MysqlDialect) ListRoutines(ctx context.Context, q dao.Querier, schema string) ([]dao.RoutineInfo, error) {
	// The PARAMETERS join matches ROUTINE_TYPE too, and accumulation keys
	// on schema+name+TYPE: MySQL permits a FUNCTION and a PROCEDURE with
	// the same name, and they must stay distinct rows (MF11).
	const stmt = `SELECT r.ROUTINE_SCHEMA, r.ROUTINE_NAME, r.ROUTINE_TYPE,
			COALESCE(p.ORDINAL_POSITION, -1),
			COALESCE(p.PARAMETER_MODE, ''), COALESCE(p.PARAMETER_NAME, ''),
			COALESCE(p.DTD_IDENTIFIER, '')
		FROM information_schema.ROUTINES r
		LEFT JOIN information_schema.PARAMETERS p
			ON p.SPECIFIC_SCHEMA = r.ROUTINE_SCHEMA
			AND p.SPECIFIC_NAME = r.SPECIFIC_NAME
			AND p.ROUTINE_TYPE = r.ROUTINE_TYPE
		WHERE r.ROUTINE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE())
		ORDER BY r.ROUTINE_SCHEMA, r.ROUTINE_NAME, r.ROUTINE_TYPE, p.ORDINAL_POSITION`
	rows, err := q.QueryContext(ctx, stmt, schema)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()

	type acc struct {
		info   dao.RoutineInfo
		params []string
		ret    string
	}
	var order []string
	byName := map[string]*acc{}
	for rows.Next() {
		var rschema, rname, rtype, mode, pname, dtd string
		var pos int
		if err := rows.Scan(&rschema, &rname, &rtype, &pos, &mode, &pname, &dtd); err != nil {
			return nil, err
		}
		key := rschema + "." + rname + "." + rtype
		a, ok := byName[key]
		if !ok {
			kind := dao.RoutineKindFunction
			if rtype == "PROCEDURE" {
				kind = dao.RoutineKindProcedure
			}
			a = &acc{info: dao.RoutineInfo{Schema: rschema, Name: rname, Kind: kind}}
			byName[key] = a
			order = append(order, key)
		}
		switch {
		case pos == 0: // the return row (functions only)
			a.ret = dtd
		case pos > 0:
			// Render "MODE name type" with EVERY non-empty mode — IN
			// included, per the ADR's rendering rule (MF11).
			part := dtd
			if pname != "" {
				part = pname + " " + dtd
			}
			if mode != "" {
				part = mode + " " + part
			}
			a.params = append(a.params, part)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]dao.RoutineInfo, 0, len(order))
	for _, key := range order {
		a := byName[key]
		sig := "(" + strings.Join(a.params, ", ") + ")"
		if a.info.Kind == dao.RoutineKindFunction && a.ret != "" {
			sig += " -> " + a.ret
		}
		a.info.Signature = sig
		out = append(out, a.info)
	}
	return out, nil
}
