package sqlite

import (
	"context"

	"github.com/yongjohnlee80/golib/dao"
)

// Schema introspection for SQLite.
//
// SQLite exposes its catalog through sqlite_master and the table-valued pragma
// functions. Table names stay BOUND PARAMETERS here, because the driver
// supports binding pragma-function arguments and a bound value can never be
// read as SQL.
//
// The schema (database) name is the exception: SQLite accepts no bind in that
// position anywhere, so it has to be interpolated, and it is quoted as an
// identifier through the dialect first. That quoting is the only thing
// standing between a caller-supplied schema name and injected SQL, so it must
// not be skipped for a name that "looks safe".
// REFERENCE: https://www.sqlite.org/pragma.html

// ListSchemas lists the attached databases ("main", "temp", and any ATTACHed
// name), in attachment order.
func (SqliteDialect) ListSchemas(ctx context.Context, q dao.Querier) ([]dao.SchemaInfo, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_database_list ORDER BY seq`)
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

// ListTables lists the tables and views of the given database. An empty
// schema means "main". SQLite's internal sqlite_% objects are excluded.
func (d SqliteDialect) ListTables(ctx context.Context, q dao.Querier, schema string) ([]dao.TableInfo, error) {
	if schema == "" {
		schema = "main"
	}
	// The database name cannot be bound; it is quoted as an identifier.
	stmt := `SELECT type, name FROM ` + d.QuoteIdent(schema) + `.sqlite_master
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		ORDER BY name`
	rows, err := q.QueryContext(ctx, stmt)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []dao.TableInfo
	for rows.Next() {
		var typ, name string
		if err := rows.Scan(&typ, &name); err != nil {
			return nil, err
		}
		kind := dao.TableKindTable
		if typ == "view" {
			kind = dao.TableKindView
		}
		out = append(out, dao.TableInfo{Schema: schema, Name: name, Kind: kind})
	}
	return out, rows.Err()
}

// ListColumns lists the columns of schema.table via the bindable
// pragma_table_info table-valued function, in declaration (cid) order. An
// empty schema means "main". pk > 0 marks primary-key membership.
func (SqliteDialect) ListColumns(ctx context.Context, q dao.Querier, schema, table string) ([]dao.ColumnInfo, error) {
	if schema == "" {
		schema = "main"
	}
	const stmt = `SELECT name, type, "notnull", dflt_value, cid, pk
		FROM pragma_table_info(?, ?) ORDER BY cid`
	rows, err := q.QueryContext(ctx, stmt, table, schema)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []dao.ColumnInfo
	for rows.Next() {
		var c dao.ColumnInfo
		var notnull, cid, pk int
		var def *string
		if err := rows.Scan(&c.Name, &c.DataType, &notnull, &def, &cid, &pk); err != nil {
			return nil, err
		}
		// A primary-key column is reported as NOT nullable, whatever SQLite
		// says about it. Two SQLite behaviours make the raw answer misleading:
		// pragma_table_info reports notnull=0 for an INTEGER PRIMARY KEY
		// (which is an alias for the rowid and can never be null), and older
		// SQLite genuinely permits NULLs in some non-INTEGER primary keys.
		// Passing either through would tell a caller that a key column is
		// optional, so both are normalised away here.
		// REFERENCE: https://www.sqlite.org/lang_createtable.html
		c.Nullable = notnull == 0 && pk == 0
		if def != nil {
			c.Default, c.HasDefault = *def, true
		}
		c.Position = cid + 1 // cid is 0-based
		c.PrimaryKey = pk > 0
		out = append(out, c)
	}
	return out, rows.Err()
}
