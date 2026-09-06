// Package mysql implements the dao driver for MySQL (and compatible servers)
// over github.com/go-sql-driver/mysql — a pure-Go database/sql driver, so the
// stdlib *sql.Rows / sql.Result satisfy dao.Rows / dao.Result (and
// dao.RowsColumns) natively.
//
// It is the package's first LastInsertId-profile dialect (golib-dao): no
// INSERT... RETURNING, ids come from the OK packet. Upserts render
// as ON DUPLICATE KEY UPDATE, which fires on ANY unique-key conflict — the
// conflict target cannot be narrowed to specific columns.
//
// Open takes a go-sql-driver DSN, e.g.
//
//	user:pass@tcp(localhost:3306)/appdb?parseTime=true
//
// parseTime=true is recommended so DATE/DATETIME columns scan into time.Time.
package mysql
