// Package dao is a generic, driver-agnostic data-access layer (DAL) for Go.
//
// The package owns CRUD on declared entities: each entity is described once
// (its fields, columns, scan targets, joins, sort, and search config) and that
// single declaration drives column selection, scanning, query building, and
// batch writes. It is deliberately not an ORM — no struct-tag magic, no
// migrations, explicit columns and explicit joins.
//
// # Driver boundary
//
// All query logic is written against two interfaces, [DataConn] and [Dialect],
// so the engine never branches on the database. Anything that differs between
// databases — placeholder syntax, identifier quoting, the bind-parameter
// ceiling, RETURNING/upsert support, bulk-load capability, and error encoding —
// lives behind [Dialect]. Concrete drivers live in sub-packages (for example
// dao/postgres); the core has zero external dependencies and executes through
// the standard-library database/sql shapes captured by [Querier] and [Execer].
//
// [GenericDialect] is a zero-dependency Dialect used as a test base and as a
// reasonable default for database/sql drivers that follow Postgres/SQLite
// conventions.
//
// # Batch writes
//
// [BatchWriter] (obtained from a DAO via Batch) accumulates rows and flushes
// them as the minimum number of statements that respect the dialect's
// MaxBindParams limit, with an optional COPY fast-path. Chunking is automatic
// and driver-aware, so the Postgres 65535 bind-parameter limit cannot be
// exceeded by construction.
//
// # Errors
//
// Driver errors are translated at the boundary (see [Dialect.TranslateError])
// into the package sentinels ([ErrNoRows], [ErrDuplicate], [ErrNotNull],
// [ErrForeignKey]) and, for constraint violations, a [ConstraintError] carrying
// the constraint name and [ConstraintKind].
//
// This package is built incrementally per the ADR-0001…0007 design set; see the
// dao design dossier for the full contract.
package dao
