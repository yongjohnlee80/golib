# ADR-0014 — `golib/dao`: routine introspection (functions & procedures)

- **Status:** Proposed (2026-08-16; authored by ultron-prime for autodb M6.
  The DB explorer lists "tables, views, functions and so on" — ADR-0013's
  `Introspector` covers tables/views/columns only. Lands on `tui-m6`.)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive)
- **Related:** ADR-0013 (schema introspection), the
  `interface-evolution-capability-interfaces` KB convention (why this is a
  NEW capability interface, not growth of `Introspector`)

## 1. Context

`dao.Introspector` is a published capability interface; growing it would
silently break external implementors via the method-promotion hazard the
M1 review codified. Routines are therefore a sibling capability.

## 2. Decision

```go
// RoutineKind classifies a stored routine.
type RoutineKind string
const (
	RoutineKindFunction  RoutineKind = "function"
	RoutineKindProcedure RoutineKind = "procedure"
)

type RoutineInfo struct {
	Schema string
	Name   string
	Kind   RoutineKind
	// Signature is a display-oriented rendering: "(args) -> result" for
	// functions, "(args)" for procedures (no result part — r3, both
	// drivers). The routine NAME is not repeated in it, and it is never
	// parsed.
	Signature string
}

// RoutineIntrospector is an OPTIONAL capability implemented by a DIALECT
// and probed through conn.Dialect() — mirroring ADR-0013's Introspector
// seam EXACTLY (r2; the earlier "DataConn-level" phrasing was wrong: a
// dialect executes catalog queries through the Querier it is handed).
type RoutineIntrospector interface {
	ListRoutines(ctx context.Context, q Querier, schema string) ([]RoutineInfo, error)
}

// Capability probe, mirroring the ADR-0013 helpers:
func SupportsRoutineIntrospection(d Dialect) bool

// Package-level prober, mirroring dao.ListTables: probes conn.Dialect(),
// passes conn as the Querier.
func ListRoutines(ctx context.Context, conn DataConn, schema string) ([]RoutineInfo, error)
// → ErrUnsupported when the dialect lacks the capability.
```

**Driver projections (r2 — complete, ordered, overload-safe):**

- **postgres:** `pg_proc` joined to `pg_namespace`; `prokind IN ('f','p')`
  (aggregates `'a'` and window `'w'` excluded in v1);
  `Signature = "(" || pg_get_function_arguments(oid) || ") -> " ||
  pg_get_function_result(oid)` (procedures render `-> void`-less: just
  the argument list). Ordering: `ORDER BY nspname, proname,
  pg_get_function_arguments(oid)` — overloads are DISTINCT rows,
  deterministically ordered by their argument rendering.
- **mysql:** `information_schema.ROUTINES` joined to
  `information_schema.PARAMETERS` (`ORDER BY ORDINAL_POSITION`) —
  parameters render as `MODE name type` comma-joined; position 0 (the
  return row, functions only) renders after `->`. Ordering:
  `ROUTINE_SCHEMA, ROUTINE_NAME` (MySQL has no overloads).
- **sqlite** — NOT implemented (SQLite has no stored routines; the
  prober returns `ErrUnsupported`, which consumers render as an absent
  section, never an error). `dao/bigquery` (nested module) is untouched;
  its interface-completion debt note in ADR-0013 §5 gains this row.

## 3. Files / acceptance

`dao/introspect.go` (+types, `SupportsRoutineIntrospection`, prober),
`dao/postgres/introspect.go`, `dao/mysql/introspect.go`, tests beside the
existing introspection suites (postgres live-gated on TEST_PGURL —
including an overload pair asserting distinct rows and stable order —
mysql on TEST_MYSQL_DSN, capability probe unit tests everywhere).
GenericDialect implements nothing (per the capability convention); every
existing dialect is untouched except postgres/mysql gaining the method.
