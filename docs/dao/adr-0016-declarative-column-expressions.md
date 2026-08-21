# ADR-0016 — `golib/dao`: declarative column expressions

- **Status:** **Proposed** (2026-08-21 — authored by jarvis from Johno's request for
  a SQL helper surface so declarations can be built from the table and field
  constants that already exist, instead of restating them as string literals.
  Awaiting lector design review; lands on `dao-expr`.)
- **Date:** 2026-08-21
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none — purely additive. Extends the ADR-0002/0003 column-targeting
  contract and the ADR-0006 declaration surface; `Field.Column` keeps its exact
  meaning and behavior.
- **Related:** ADR-0013 (per-part identifier quoting; `TableQuoter`), ADR-0010
  (`WriteColumn`/write derivation), ADR-0004 (`Dialect.QuoteIdent`), the
  `interface-evolution-capability-interfaces` KB convention, [[golib]] Part 2
  (functional options; no dead exported surface; explicit over magic)

## 1. Context

`Field.Column` is a raw SQL expression **emitted verbatim** into the projection
(`sql.go` `buildSelect`: *"cols are already-resolved SQL expressions
(Field.Column), emitted verbatim"*). Writes take a different path: `writeCol()`
derives the bare name as the tail after the last dot and the builder quotes it
with `Dialect.QuoteIdent`; table positions go through `quoteTable`.

So a declaration is a hand-written string. What that costs, measured across the
three `golib/dao` consumers in this workspace:

| Consumer | `Column` declarations | Shape | What the strings cost |
| --- | --- | --- | --- |
| `ddex-server` (`store/`) | **113** | all plain `table.col`, **8 distinct tables** | the table name is restated on every line — `track.` 29×, `release.` 23×, `delivery.` 19×, `deal.` 14×. Renaming a table is 113 unchecked string edits |
| `autodb` (`core/meta`) | **57** | all **bare, unqualified** (`"action"`, `"cidr"`, `"dsn_enc"`) | single-table entities with no joins — nothing to qualify, but nothing quoted either |
| `example/` (the pattern golib ships) | **17** | **all 17 hand-quoted**: `` `"user".first_name` `` | `user` is reserved in Postgres, so every literal carries ANSI double quotes — correct on pg/sqlite, **wrong on MySQL** (backticks) |

Two conclusions, and the second is the one that shapes this ADR:

1. **The constants already exist.** Every consumer declares a field enum
   (`ArtistField`, `MetaKVField`) and names its table in `dao.Table(...)`, then
   restates both inside string literals the compiler never checks against them.
2. **Identifier quoting is necessary, dialect-specific, and impossible to write
   correctly by hand in a portable declaration.** The one declaration in the
   shipped example that *needed* care is the one the raw string got wrong for
   MySQL. And it cannot be fixed with a smarter string, because a package-level
   `map[Field]dao.Field[R]` is initialized long before any `DataConn` exists.

`dao.New(conn, opts...)` **does** have the dialect. Everything that consumes
`Field.Column` — `resolve`, `column`, `writeCol`, the search-op column binding,
`sortExpr` — runs at or after `New`. That is the seam this ADR uses.

Prior art for the shape: LM's legacy DAL restated each column three times
(enum / translator / scanner), which ADR-0003 already collapsed into one
`Field`. This ADR finishes the job for the *expression* half — and `COALESCE`
appears **324 times** in that legacy DAL, so the joined-column form is not a
corner case.

## 2. Decision

### 2.1 `Expr` — an expression that resolves against a dialect

```go
// Expr is a SQL expression that can only be rendered once a dialect is known.
// Declarations hold Exprs; dao.New resolves them exactly once (ADR-0016 §2.2).
// Zero dependencies, no reflection, no state: an Expr is a pure function.
type Expr func(Dialect) string
```

### 2.2 `Field.Expr`, resolved once at `New`

```go
type Field[R any] struct {
	// Column is the SQL expression projected for this field ... (unchanged)
	Column string

	// Expr is the dialect-resolved alternative to Column: dao.New renders it
	// once via the connection's Dialect and stores the result as Column, so
	// everything downstream is byte-identical to a hand-written Column.
	// Setting both Column and Expr is a declaration error (panics at New).
	Expr Expr

	// ... all other fields unchanged
}
```

At `New`, after options are applied and before validation:

```go
for key, f := range cfg.fields {
	if f.Expr == nil {
		continue
	}
	if f.Column != "" {
		panic(fmt.Sprintf("dao.New: field %q sets both Column and Expr", any(key)))
	}
	f.Column = f.Expr(conn.Dialect())
	cfg.fields[key] = f
}
```

This is deliberately the **smallest possible seam**: one resolution pass, after
which `Field.Column` is exactly what a hand-written declaration would have
produced. No query-time cost, no second code path in the builder, no change to
`resolve`/`writeCol`/search binding/`sortExpr`.

### 2.3 The helper set (v1)

Generic over `~string` so the field enums and table constants that already exist
pass without conversion:

```go
// T qualifies a column with its table: T(TableArtist, ArtistName) renders
// "artist"."name" on Postgres and `artist`.`name` on MySQL. Each part is quoted
// separately, matching ADR-0013's per-part treatment of qualified names.
func T[Tbl ~string, Col ~string](table Tbl, col Col) Expr

// C is an unqualified column: C(MetaKVKey) renders "key" / `key`. Single-table
// entities with no joins need no qualification (autodb's 57 declarations) but
// still benefit from quoting.
func C[Col ~string](col Col) Expr

// Lit is a SQL literal: strings render single-quoted with embedded quotes
// doubled, numbers verbatim, bool as TRUE/FALSE, nil as NULL. It rejects any
// other type IMMEDIATELY (panic at declaration, i.e. package init) rather than
// at New — the earliest possible failure for a value that is always a compile-
// time constant in practice.
func Lit(v any) Expr

// Coalesce renders COALESCE(e, alt). alt is an Expr when you pass one, and a
// Lit otherwise, so the common Coalesce(T(t, c), "") reads as intended.
func Coalesce(e Expr, alt any) Expr

// SQL is the escape hatch: text is emitted verbatim, unquoted, unresolved.
// For expressions the helpers do not cover (NOW(), a window function, a
// dialect-specific cast). Named SQL because dao.Raw is already the
// predicate-position escape hatch (query.go).
func SQL(text string) Expr

// LeftJoin and InnerJoin render a join clause for OptionalJoinExpr:
//   LeftJoin(TableLabelGroup, T(TableLabelGroup, LabelGroupID), T(TableArtist, ArtistLabelGroupID))
// -> LEFT JOIN "label_group" ON "label_group"."id" = "artist"."label_group_id"
// The table is rendered in TABLE position (quoteTable, so a schema-qualified
// name splits correctly on a TableQuoter dialect); the operands are Exprs.
func LeftJoin[Tbl ~string](table Tbl, left, right Expr) Expr
func InnerJoin[Tbl ~string](table Tbl, left, right Expr) Expr
```

The resulting declaration — the shape Johno asked for:

```go
const (
	TableArtist     = "artist"
	TableLabelGroup = "label_group"
)

var artistFields = map[ArtistField]dao.Field[*Artist]{
	ArtistID:     {Expr: dao.T(TableArtist, ArtistID), Scan: sID, Value: vID},
	ArtistName:   {Expr: dao.T(TableArtist, ArtistName), Scan: sName, Value: vName},
	ArtistLabelGroup: {
		Expr:     dao.Coalesce(dao.T(TableLabelGroup, LabelGroupName), ""),
		Scan:     sLabel,
		Join:     TableLabelGroup,
		ReadOnly: true,
	},
}
```

Note `Join: TableLabelGroup` works because `JoinKey` is `~string` and
`TableLabelGroup` is an untyped constant — using the table name as its own join
key needs no extra machinery, and the ADR recommends it as the convention.

### 2.4 Quoting rules

`T`/`C` quote through `Dialect.QuoteIdent`, **per part** — the same treatment
ADR-0013 gave qualified table names, so behavior is consistent with the rest of
the engine. `LeftJoin`/`InnerJoin` render their table through the same
`quoteTable` helper the builder uses, so a `TableQuoter` dialect splits
`schema.table` correctly and a non-implementer keeps historical behavior.

**Quoting is not semantically neutral, and this is the one hazard worth
stating.** In Postgres an unquoted identifier folds to lower case while a quoted
one is exact, so `C("MyCol")` renders `"MyCol"` and will NOT match a column
created unquoted as `MyCol` (stored as `mycol`). Every identifier in all 187
declarations surveyed (113 + 57 + 17) is lower-case `snake_case` — the only
upper-case text in any of them is the `COALESCE` keyword itself — so quoting is
a provable no-op for the entire evidence base — but a mixed-case declaration must migrate to `SQL("MyCol")` or stay a
plain `Column` string. Migration is opt-in per field (§5), so nothing changes
until a declaration is rewritten.

### 2.5 `OptionalJoinExpr`

```go
// OptionalJoinExpr registers an optional join whose clause is dialect-resolved
// at New. It is the Expr sibling of OptionalJoin; the string form is unchanged.
func OptionalJoinExpr[R any, C ~string, K ~string, ID any](key JoinKey, e Expr) Option[R, C, K, ID]
```

A sibling option rather than a widened `OptionalJoin` signature: Go has no
overloading, and taking `any` to accept "string or Expr" would be exactly the
untyped magic [[golib]] Part 1 §4 rules out. `OptionalJoin` keeps working
verbatim.

### 2.6 Write-column safety — a fail-early check the helpers make necessary

`writeCol()` derives the write column as the tail after the last dot. For
`COALESCE("label_group"."name", '')` that tail is `"name", '')` — meaningless
SQL. Today that is latent: such fields are `ReadOnly` by convention, and nothing
enforces it. `Coalesce` makes expression-valued columns easy to write, so the
convention becomes a check:

```
dao.New panics when a field's derived write column is not a plain identifier
(it contains whitespace, parentheses, commas or quotes) AND the field is
writable (not ReadOnly) AND WriteColumn is not set.
```

The message names the field, the derived garbage, and the two ways out
(`ReadOnly: true` or an explicit `WriteColumn`). It converts a broken INSERT at
run time into a panic at construction — the [[golib]] "misconfiguration fails at
construction" rule. It fires only on declarations that are already broken.

### 2.7 Not in v1 — deliberately

- **`Lower`/`Upper`/`Concat`/`Cast`/aggregates.** LM's legacy DAL uses `UPPER(`
  58×, `LOWER(` 27×, `CONCAT` 27×, `SUM(` 55× — but overwhelmingly in
  *predicate* and *aggregate* position, which the DAO already owns (`Search`,
  `Count`, `WithPredicate`), not in a declared projection. Adding them now would
  be exported surface with no caller, which [[golib]] forbids. `SQL()` covers
  them until a real declaration needs one.
- **`SortMap` and search-op `Expr` variants.** Neither `ddex-server` nor
  `autodb` uses `SortMap` at all, and `example/` has two search ops. Same
  dead-surface reasoning; both are additive later, and `SortMap` values are
  resolved at `New` too, so the seam is already in the right place.
- **Predicate-position `Expr`.** `dao.Raw` already covers it and predicates are
  built at query time, where the dialect is available anyway.

## 3. What does not change

- `Field.Column` — meaning, type, and verbatim emission. A declaration that
  never sets `Expr` behaves identically, byte for byte.
- `writeCol()` derivation, `WriteColumn`, `Clearable`/`ClearValue`,
  `ReadOnly`, `Join`.
- `Dialect` (no new methods — `Expr` consumes the existing `QuoteIdent` and the
  optional `TableQuoter`), `DataConn`, the builder, the DAO surface.
- `OptionalJoin`, `SortMap`, `Search`, and every other option.
- Query-time cost: zero. Resolution happens once per schema at `New`.

## 4. Alternatives considered

1. **Plain string helpers** (`func T(...) string`, Johno's first sketch). Half
   the diff and no `Field` change. Rejected: with no dialect they cannot quote,
   so the reserved-word case that the shipped `example/` already gets wrong for
   MySQL stays wrong, and every consumer keeps hand-writing dialect-specific
   literals. Johno chose the resolved form once the trade-off was concrete.
2. **String helpers that quote with ANSI `"` by default.** Correct for
   Postgres/SQLite, wrong for MySQL — a dialect baked into the dialect-agnostic
   core. Rejected outright.
3. **Change `Field.Column`'s type to `Expr`.** One way to declare a column
   instead of two, but it breaks every existing declaration at once (187 in this
   workspace) for no behavioral gain, and forces `SQL("…")` wrapping on all of
   them. The additive field costs one precedence rule (§2.2) and lets migration
   be per-field.
4. **A dot-importable `dao/expr` subpackage** so call sites read bare `T(...)`
   as sketched. Johno's call: *"we can have these methods in either sql_ext.go or
   helper.go or whatever make most sense"* — same package, new file.
   `dao.T(...)` it is; anyone wanting brevity can alias locally.
5. **Resolving at query time instead of at `New`.** Would allow one declaration
   to serve two dialects, which no consumer needs (a `Schema` is built from one
   `DataConn` and holds it for the process lifetime, ADR-0006), and it would put
   a closure call in every statement's hot path against [[golib]] principle 5.

## 5. Migration

**Nothing is forced.** `Column` and `Expr` coexist; a declaration migrates when
someone rewrites it, one field at a time.

| Before | After |
| --- | --- |
| `{Column: "artist.name", …}` | `{Expr: dao.T(TableArtist, ArtistName), …}` |
| `{Column: "action", …}` | `{Expr: dao.C(MetaKVAction), …}` |
| `` {Column: `"user".first_name`} `` | `{Expr: dao.T(TableUser, UserFirstName)}` — and now correct on MySQL too |
| `{Column: "COALESCE(label_group.name,'')", ReadOnly: true}` | `{Expr: dao.Coalesce(dao.T(TableLabelGroup, LabelGroupName), ""), ReadOnly: true}` |
| `dao.OptionalJoin(k, "LEFT JOIN label_group ON …")` | `dao.OptionalJoinExpr(k, dao.LeftJoin(TableLabelGroup, dao.T(…), dao.T(…)))` |
| mixed-case identifier | keep `Column`, or `dao.SQL("MyCol")` — see §2.4 |

In-repo work for this ADR: golib's own `dao/USAGE.md` §1 and `dao/README.md`
gain the resolved form (they are the surface every consumer copies), plus the
`example/` model files as the executable demonstration (**separate repository** —
a coordinated companion commit, as in ADR-0015 §5). `ddex-server` (113) and
`autodb` (57) are separate repositories on their own release cadence: they
migrate when they choose, and this ADR does not touch them.

## 6. Files / acceptance

New: `dao/expr.go` (the `Expr` type and the helper set), `dao/expr_test.go`.
Changed: `dao/field.go` (`Field.Expr`), `dao/schema.go` (resolution pass +
both-set panic + §2.6 write-column check), `dao/options.go`
(`OptionalJoinExpr`), `dao/README.md`, `dao/USAGE.md`, `dao/doc.go` if it
enumerates the declaration surface.

Acceptance criteria:

1. `T`/`C` render per-part `QuoteIdent` on `GenericDialect`, postgres (`"a"."b"`),
   mysql (`` `a`.`b` ``) and sqlite; a typed field enum and an untyped string
   constant both compile as arguments without conversion.
2. A `Field` with `Expr` set and `Column` empty produces, after `New`, a
   `Column` byte-identical to the equivalent hand-written literal — asserted by
   building two schemas (one of each form) and diffing the emitted SELECT.
3. Setting both `Column` and `Expr` panics at `New`, naming the field.
4. `Coalesce(T(t, c), "")` renders `COALESCE("t"."c", '')`; `Coalesce(e, Lit(0))`
   and `Coalesce(e, 0)` render identically; a string literal containing a single
   quote is doubled, not escaped with a backslash.
5. `Lit` panics immediately for an unsupported type (a struct), not at `New`.
6. `LeftJoin`/`InnerJoin` render the table through `quoteTable`: a
   `TableQuoter` dialect splits `schema.table`, a non-implementer quotes the
   whole string — the ADR-0013 fallback, unchanged.
7. `OptionalJoinExpr` produces a join applied on exactly the same demand-driven
   triggers as `OptionalJoin` (selected column, sort, `DAO.Join`) — the existing
   join tests pass against both forms.
8. §2.6: a writable field whose derived write column is not a plain identifier
   panics at `New`; the same field with `ReadOnly: true`, or with an explicit
   `WriteColumn`, constructs fine. Every existing declaration in golib's tests
   and in `example/` still constructs.
9. `SQL("NOW()")` is emitted verbatim, unquoted.
10. Zero query-time cost: an allocation benchmark over a tx-free `Select` shows
    no change against the string-declared schema.

## 7. Open questions for review

1. **`T` and `C` as exported names.** They are two of the shortest possible
   exports in a package that also uses `C` as a type-parameter name for the
   field enum (`Schema[R, C, K, ID]`). Different scopes, so it compiles — but is
   `dao.T` / `dao.C` the right readability trade against `dao.Col` /
   `dao.Column`? Johno sketched `T()`; confidence it should stay: 70%.
2. **`Lit`'s panic-at-declaration.** Package-level `var` init means the panic
   surfaces at program start with a stack in `init`. Right call versus deferring
   to `New` (where the field name is known and the message can be better)?
3. **§2.6's new panic.** It fires on declarations that are already broken, but
   it *is* a new construction-time failure in a released package. Ship it with
   this ADR, or split it into its own change so this one stays purely additive?
4. **`Coalesce(e, alt any)`** takes `any` to make `Coalesce(x, "")` read well,
   at the cost of accepting `Coalesce(x, someStruct{})` that only fails inside
   `Lit`. Worth it, or should it be `Coalesce(e, alt Expr)` and force
   `Lit("")`?
5. **Scope check (§2.7):** are `Lower`/`Upper`/`Concat`/`Cast` genuinely better
   deferred to `SQL()`, given LM's counts are in predicate rather than
   projection position?

## Review history

- **r1 (pending)** — lector design review requested 2026-08-21.
