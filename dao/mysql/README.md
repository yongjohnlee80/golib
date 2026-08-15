# dao/mysql

The dao driver for MySQL (and compatible servers), over the pure-Go
[`go-sql-driver/mysql`](https://github.com/go-sql-driver/mysql).

## Install

```bash
go get github.com/yongjohnlee80/golib
```

## Features

- `database/sql`-backed `dao.DataConn`: `Open` / `OpenNamed`, pool options
  (`MaxOpenConns`, `MaxIdleConns`, `ConnMaxLifetime`, `ConnMaxIdleTime`),
  ping-on-open.
- **LastInsertId profile** (golib-dao ADR-0008 §2.6): no `RETURNING`; insert
  ids come from the OK packet. `ID` must be `int64` for generated ids.
- **Upserts** via `ON DUPLICATE KEY UPDATE` (ADR-0011): fires on *any*
  unique-key conflict — the conflict target cannot be narrowed. Skip-conflict
  batches use the self-assignment do-nothing idiom.
- **Schema introspection** (ADR-0013): `dao.ListSchemas` / `ListTables` /
  `ListColumns` over `information_schema`.
- **Result column names** (ADR-0012): `*sql.Rows` satisfies `dao.RowsColumns`
  natively.
- Errno error translation: duplicate (1062/1586), not-null (1048), foreign
  key (1216/1217/1451/1452), check (3819) → dao sentinels.

## Example

```go
conn, err := mysql.Open(ctx, "user:pass@tcp(localhost:3306)/appdb?parseTime=true",
	mysql.MaxOpenConns(8))
if err != nil { ... }
defer conn.Close()

schema := dao.New(conn, /* field declarations */)
```

`parseTime=true` is recommended so `DATE`/`DATETIME` columns scan into
`time.Time`.

## Integration tests

Gated on `TEST_MYSQL_DSN` (skip cleanly when unset):

```bash
TEST_MYSQL_DSN='root:secret@tcp(localhost:3306)/example?parseTime=true' go test ./dao/mysql/
```

## License

[MIT](../../LICENSE)
