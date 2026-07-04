// Package postgres is the reference Postgres driver for golib/dao, implemented
// over github.com/jackc/pgx/v5.
//
// [Open] / [OpenNamed] return a dao.DataConn backed by a pgx connection pool.
// [PostgresDialect] provides $n placeholders, the 65535 bind-parameter limit,
// RETURNING and ON CONFLICT, the native CopyFrom bulk-load fast-path, and
// SQLSTATE -> dao sentinel error translation.
//
// Two-phase commit is supported: TwoPhaseSupported reports true and the
// PREPARE TRANSACTION / COMMIT PREPARED / ROLLBACK PREPARED trio is
// implemented (with pgx connection release after prepare, and a command-tag
// check that rejects a PREPARE silently rolled back in an aborted
// transaction). The PostgreSQL server must be started with
// max_prepared_transactions > 0 (its default is 0); otherwise the prepare
// fails and dao.RunTx reports it.
package postgres
