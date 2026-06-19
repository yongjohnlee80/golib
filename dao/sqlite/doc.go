// Package sqlite is a SQLite driver for golib/dao, implemented over the standard
// library database/sql and backed by the pure-Go modernc.org/sqlite driver (no
// cgo). Importing this package registers that driver.
//
// [Open] / [OpenNamed] return a dao.DataConn over a *sql.DB. [SqliteDialect]
// embeds dao.GenericDialect and overrides only the SQLite-specific bits: "?"
// placeholders, the 999 bind-parameter limit, and SQLite result-code error
// translation. RETURNING and ON CONFLICT (inherited from GenericDialect) work on
// modern SQLite (3.35+, which modernc bundles); there is no COPY fast-path, so
// batches use the chunked multi-row INSERT path.
//
// Because modernc.org/sqlite runs in-process, this driver is convenient for tests
// (use an on-disk temp file, or ":memory:" with MaxOpenConns(1)).
package sqlite
