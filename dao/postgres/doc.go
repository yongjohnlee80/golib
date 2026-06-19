// Package postgres is the reference Postgres driver for golib/dao, implemented
// over github.com/jackc/pgx/v5. It is the only golib package that carries a
// database dependency; the dao core stays zero-dependency.
//
// [Open] / [OpenNamed] return a dao.DataConn backed by a pgx connection pool.
// [PostgresDialect] provides $n placeholders, the 65535 bind-parameter limit,
// RETURNING and ON CONFLICT, the native CopyFrom bulk-load fast-path, and
// SQLSTATE -> dao sentinel error translation.
//
// Two-phase commit: PostgreSQL supports prepared transactions, but safe execution
// (releasing the pgx connection after PREPARE TRANSACTION, COMMIT PREPARED on a
// fresh connection, and reaping orphaned prepared transactions) is a deliberate
// follow-up. Until then TwoPhaseSupported reports false, so dao.Transaction.TwoPhase
// fails fast rather than silently degrading.
package postgres
