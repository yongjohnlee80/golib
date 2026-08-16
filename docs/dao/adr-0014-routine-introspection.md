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
	// Signature is the engine's human-readable argument/return rendering
	// (display-oriented; never parse it).
	Signature string
}

// RoutineIntrospector is an OPTIONAL capability probed by type assertion.
type RoutineIntrospector interface {
	ListRoutines(ctx context.Context, schema string) ([]RoutineInfo, error)
}

// Package-level prober, mirroring dao.ListTables:
func ListRoutines(ctx context.Context, conn DataConn, schema string) ([]RoutineInfo, error)
// → ErrUnsupported when the driver lacks the capability.
```

Drivers: **postgres** (`pg_proc` joined to `pg_namespace`, `prokind` →
function/procedure, `pg_get_function_arguments` for Signature; excludes
aggregates/window in v1), **mysql** (`information_schema.ROUTINES`),
**sqlite** — NOT implemented (SQLite has no stored routines; the prober
returns `ErrUnsupported`, which the explorer renders as an absent
section, never an error). `dao/bigquery` (nested module) is untouched;
its interface-completion debt note in ADR-0013 §5 gains this row.

## 3. Files / acceptance

`dao/introspect.go` (+types/prober), `dao/postgres/introspect.go`,
`dao/mysql/introspect.go`, tests beside the existing introspection suites
(postgres live-gated on TEST_PGURL, mysql on TEST_MYSQL_DSN, capability
probe unit tests everywhere). GenericDialect and every dialect stay
untouched — this is DataConn-level, like ADR-0013.
