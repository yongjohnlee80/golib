# ADR-0016 — `golib/dao`: declarative column expressions

- **Status:** **Proposed (rev 3)** (2026-08-21 — authored by jarvis from Johno's
  request for a SQL helper surface so declarations can be built from the table
  and field constants that already exist, instead of restating them as string
  literals. Lector design r1 `change_requested` folded — including a fatal
  write-path defect in rev 0 — then rev 2 restored the literal sugar at Johno's
  direction, and rev 3 moved its "string or int only" rule from a declaration
  panic to a compile-time constraint. See Review history. Lands on `dao-expr`.)
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

### 2.1 `Expr` — a resolvable expression that also carries its write identity

An `Expr` cannot be a bare `func(Dialect) string`. The write path does not use
the projected expression: `writeCol()` takes the tail after the last dot and
`INSERT`/`UPDATE` pass it through `QuoteIdent` again. A rendered, already-quoted
projection therefore double-quotes — measured against the real code:

```text
Column  artist.name                  -> writeCol  name          -> INSERT emits "name"          OK (today)
Column  "artist"."name"              -> writeCol  "name"        -> INSERT emits """name"""      INVALID
Column  `artist`.`name`              -> writeCol  `name`        -> INSERT emits "`name`"        INVALID
Column  COALESCE("lg"."name", '')    -> writeCol  "name", '')   -> INSERT emits """name"", '')" INVALID
```

So every writable `T`/`C` field in rev 0 would have produced invalid DML, and
rev 0's §2.6 plain-identifier check would have rejected each one at `New` —
including this ADR's own `ArtistID`/`ArtistName`, which declare `Value`. An
`Expr` must therefore carry the **raw, unquoted** write identity alongside the
renderer:

```go
// Expr is a SQL expression that can only be rendered once a dialect is known,
// plus the raw column identity the write path needs. Declarations hold Exprs;
// dao.New resolves them exactly once (§2.2). Construct one only through the
// helpers in §2.3 — the fields are unexported precisely so an Expr cannot be
// assembled with a write identity that disagrees with what it renders.
type Expr struct {
	render func(Dialect) string
	// write is the RAW, unquoted column name for INSERT/UPDATE, set only by T
	// and C. Every composition (Coalesce, SQL, the join helpers) leaves it
	// empty: an expression has no write identity, and inventing one would be
	// worse than having none.
	write string
}

func (e Expr) isSet() bool { return e.render != nil }
```

A zero `Expr` passed to any helper is a declaration error and panics
immediately — the fail-early rule applied at package-init time, where the
mistake is.

### 2.2 `Field.Expr`, resolved once at `New`

```go
type Field[R any] struct {
	// Column is the SQL expression projected for this field ... (unchanged)
	Column string

	// Expr is the dialect-resolved alternative to Column: dao.New renders it
	// once via the connection's Dialect and stores the result as Column, and
	// (unless WriteColumn is set explicitly) takes the raw write identity from
	// it, so everything downstream is byte-identical to a hand-written
	// declaration. Setting both Column and Expr is a declaration error
	// (panics at New).
	Expr Expr

	// ... all other fields unchanged
}
```

**Resolution writes into a schema-owned clone, never the caller's map.**
`Fields(m)` aliases the map it is handed (`options.go`: `c.fields = m`) and `New`
stores that same reference (`schema.go`: `fields: cfg.fields`), so resolving in
place would mutate a package-level `var`. A *second* `New` over the same
declaration would then see both `Column` and `Expr` set — tripping the panic
below — or silently inherit the first connection's dialect. The pass clones:

```go
// resolveFields returns a schema-owned copy of the declared fields with every
// Expr rendered against this connection's dialect. The caller's map is never
// written to: it is typically a package-level var shared by every schema built
// from it, possibly against different dialects.
func resolveFields[R any, C ~string](d Dialect, in map[C]Field[R]) map[C]Field[R] {
	out := make(map[C]Field[R], len(in))
	for key, f := range in {
		if f.Expr.isSet() {
			if f.Column != "" {
				panic(fmt.Sprintf("dao.New: field %q sets both Column and Expr", any(key)))
			}
			f.Column = f.Expr.render(d)
			// Write identity: an explicit WriteColumn always wins; otherwise T/C
			// supply the raw name, so INSERT/UPDATE quote it exactly as they
			// quote a hand-written declaration's derived tail.
			if f.WriteColumn == "" {
				f.WriteColumn = f.Expr.write
			}
		}
		out[key] = f
	}
	return out
}
```

Cloning also closes a pre-existing aliasing hazard: today a caller who mutates
their field map after `New` silently mutates the live schema.

This is deliberately the **smallest possible seam**: one resolution pass, after
which `Field.Column` and `Field.WriteColumn` are exactly what a hand-written
declaration would have produced. No query-time cost, no second code path in the
builder, no change to `resolve`/`writeCol`/search binding/`sortExpr`.

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

// Str is a string literal, and it refuses anything it cannot render
// identically on every supported dialect: a string containing a single quote,
// a backslash, or a control character panics at declaration. MySQL escaping
// depends on NO_BACKSLASH_ESCAPES and the connection charset, so the portable
// contract is not "escape correctly" but "accept only what needs no escaping"
// — which covers every literal in the evidence base ('' and simple defaults).
// Anything richer belongs in SQL().
func Str(s string) Expr

// Int is an integer literal in decimal. Floats are deliberately absent: their
// text form is precision-sensitive and non-finite values have no portable
// spelling. NULL is absent because COALESCE(x, NULL) is a no-op.
func Int(i int64) Expr

// Alt is what Coalesce accepts as its fallback: an Expr, or a string or
// integer literal. The rejection is a COMPILE error, not a runtime panic —
// Coalesce(e, 0.5) and Coalesce(e, true) do not build.
//
// The terms are deliberately NOT ~string / ~int. A tilde term admits a NAMED
// type (a field enum like ArtistField is ~string), which satisfies the
// constraint but does not match `case string` in the routing switch — the
// literal is then never built and the nil renderer segfaults at query time. A
// prototype reproduced exactly that. Excluding tildes closes the hole and buys
// a second guarantee: passing a column enum where a VALUE belongs
// (Coalesce(T(t, c), ArtistName)) stops compiling.
type Alt interface {
	Expr | string | int | int64
}

// Coalesce renders COALESCE(e, alt). alt is used directly when it is an Expr;
// otherwise it is a literal, routed through the SAME closed set and the SAME
// refusal rules — a string through Str (so a quote, a backslash or a control
// character still panics at declaration), an integer through Int. So
// Coalesce(T(t, c), "") reads as intended and cannot produce a literal this
// package would not render identically on every dialect.
//
// The routing is one unexported helper, not a public Lit(any): the exported
// literal surface stays the closed Str/Int pair, and the convenience is
// confined to this single parameter. INVARIANT: the terms of Alt and the cases
// of the routing switch are kept in lockstep — a term with no case yields a
// zero Expr, i.e. a nil renderer.
func Coalesce[A Alt](e Expr, alt A) Expr

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

`C` quotes through `Dialect.QuoteIdent`. `T` renders
`quoteTable(d, table) + "." + d.QuoteIdent(col)` — the **table part goes through
`quoteTable`**, so a schema-qualified table constant (`app.users`) splits into
two quoted parts on a `TableQuoter` dialect and falls back to whole-string
quoting on a non-implementer, exactly as ADR-0013 specified for table position.
`T` therefore **accepts** schema-qualified constants rather than rejecting them,
and inherits ADR-0013's documented fallback instead of inventing a second rule.
`LeftJoin`/`InnerJoin` render their table the same way.

The **write identity is unaffected by quoting**: `T`/`C` carry the raw column
string (§2.1), so `INSERT`/`UPDATE` see `name` and quote it exactly once —
byte-identical to what a hand-written `artist.name` produces today.

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

**Same-key precedence is later-option-wins, and setting one form deletes the
other.** The config holds the two representations in separate maps, so
`OptionalJoin(k, s)` followed by `OptionalJoinExpr(k, e)` must leave only the
Expr, and the reverse order only the string. Without the delete a stale opposite
representation would decide the clause by map-iteration accident. This is the
house option rule ("a later option overrides an earlier one for the same
setting") read across two spellings of one setting.

### 2.6 Write-column safety — a fail-early check the helpers make necessary

`writeCol()` derives the write column as the tail after the last dot. For
`COALESCE("label_group"."name", '')` that tail is `"name", '')` — meaningless
SQL. Today that is latent: such fields are `ReadOnly` by convention, and nothing
enforces it. `Coalesce` makes expression-valued columns easy to write, so the
convention becomes a check:

```
dao.New panics when a field's write column is not a plain identifier (it
contains whitespace, parentheses, commas or quotes) AND the field is writable
(not ReadOnly) AND WriteColumn is still empty after §2.2's resolution.
```

**The ordering is load-bearing, and rev 0 had it wrong.** The check must run
*after* write identity is populated, or it rejects every writable `T`/`C` field —
including this ADR's own `ArtistID`/`ArtistName`, which declare `Value`. With
resolution first, `T`/`C` fields pass (they carry a raw `WriteColumn`) and only a
genuinely unwritable declaration fails: a `Coalesce` or `SQL` expression on a
field that is neither `ReadOnly` nor given an explicit `WriteColumn`.

The message names the field, the offending write column, and the two ways out
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
- `writeCol()` derivation, `WriteColumn` (an explicit value always wins over
  §2.2's inferred one), `Clearable`/`ClearValue`, `ReadOnly`, `Join`.
- The INSERT/UPDATE path: it still quotes one raw identifier exactly once.
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
5. **`Expr` as a bare `func(Dialect) string`** (rev 0). Simpler by a struct,
   and fatally wrong: a function cannot carry the raw write identity, so the
   already-quoted render reaches `writeCol` and `INSERT`/`UPDATE` quote it a
   second time (§2.1). Any repair — parsing the quotes back off, or requiring
   every writable field to hand-write `WriteColumn` — is worse than the struct.
6. **A general `Lit(any)` literal renderer** (rev 0). Still rejected — the
   `any` in `Coalesce` (rev 2) routes through the closed `Str`/`Int` pair rather
   than reintroducing a public general literal API. Rejected on review:
   doubling single quotes is not a complete portable contract, because MySQL
   escaping also depends on `NO_BACKSLASH_ESCAPES` and the connection charset,
   and `any` leaves float/non-finite behavior undefined. The closed
   `Str`/`Int` set with a refusal rule (§2.3) covers every literal in the
   evidence base and cannot be wrong; a general literal API can land later
   against a real need.
7. **Resolving at query time instead of at `New`.** Would allow one declaration
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
Changed: `dao/field.go` (`Field.Expr`), `dao/schema.go` (`resolveFields` clone +
both-set panic + write-identity inference + §2.6 check, sequenced in that
order), `dao/options.go` (`OptionalJoinExpr` + same-key precedence across both
join representations), `dao/README.md`, `dao/USAGE.md`, `dao/doc.go` if it
enumerates the declaration surface.

Acceptance criteria:

1. `T`/`C` render per-part quoting on `GenericDialect`, postgres (`"a"."b"`),
   mysql (`` `a`.`b` ``) and sqlite; a typed field enum and an untyped string
   constant both compile as arguments without conversion. A schema-qualified
   table constant splits on a `TableQuoter` dialect and whole-string quotes on a
   non-implementer (the ADR-0013 fallback).
2. **Read AND write equivalence.** A `Field` with `Expr` set and `Column` empty
   produces, after `New`, a `Column` and `WriteColumn` byte-identical to the
   equivalent hand-written declaration — asserted by building two schemas (one
   of each form) and diffing the emitted **SELECT, INSERT, UPDATE and upsert**
   SQL. A `T`-declared writable field must emit `INSERT INTO … ("name") …`, never
   a doubly-quoted identifier (the rev-0 defect, pinned by a regression test).
3. Setting both `Column` and `Expr` panics at `New`, naming the field.
3b. **The declaration map is never mutated.** Building two schemas from the same
   package-level field map against two different dialects yields correctly
   different SQL, and the source map's `Field` values still have empty `Column`
   and non-nil `Expr` afterwards — the second `New` must not see a resolved
   `Column` and must not trip criterion 3's panic.
4. `Coalesce(T(t, c), "")`, `Coalesce(T(t, c), Str(""))` and
   `Coalesce(T(t, c), SQL("''"))` all render `COALESCE("t"."c", '')`;
   `Coalesce(e, 0)`, `Coalesce(e, int64(0))` and `Coalesce(e, Int(0))` all render
   `COALESCE(…, 0)`. `Coalesce(e, "it's")` panics under `Str`'s refusal rule.
   Every term of `Alt` produces a non-nil renderer — the lockstep invariant,
   asserted term by term so a future term added without a switch case fails a
   test rather than segfaulting a caller.
4b. `Coalesce(e, 0.5)`, `Coalesce(e, true)` and `Coalesce(e, ArtistName)` (a
   field enum) must all be **compile** errors. Mechanism is §7's open question:
   the natural form is a `testdata` fixture built by `go build` from a test,
   which costs a toolchain call — the alternative is to assert the constraint's
   shape in a comment and accept that negative typing is unverified.
5. `Str` panics immediately — at declaration, not at `New` — for a string
   containing a single quote, a backslash, or a control character; a zero `Expr`
   passed to any helper panics the same way.
6. `LeftJoin`/`InnerJoin` render the table through `quoteTable`: a
   `TableQuoter` dialect splits `schema.table`, a non-implementer quotes the
   whole string — the ADR-0013 fallback, unchanged.
7. `OptionalJoinExpr` produces a join applied on exactly the same demand-driven
   triggers as `OptionalJoin` (selected column, sort, `DAO.Join`) — the existing
   join tests pass against both forms. Same-key precedence is tested in **both
   orders**: string-then-Expr yields the Expr clause, Expr-then-string yields the
   string clause, and neither leaves a stale opposite representation.
8. §2.6: a writable field whose derived write column is not a plain identifier
   panics at `New`; the same field with `ReadOnly: true`, or with an explicit
   `WriteColumn`, constructs fine. Every existing declaration in golib's tests
   and in `example/` still constructs.
9. `SQL("NOW()")` is emitted verbatim, unquoted.
10. Zero query-time cost: an allocation benchmark over a tx-free `Select` shows
    no change against the string-declared schema.

## 7. Open question (rev 3)

**How should the negative typing be tested?** §2.3's rejections are compile
errors, and golib tests are stdlib-only. The options are (a) a `testdata`
fixture with `//go:build ignore` that a test compiles via `go build`, catching
regressions at the cost of invoking the toolchain inside `go test`; (b) leave
`Alt`'s shape asserted only by a comment plus the positive per-term test, so a
widened constraint would not be caught; or (c) a tiny `go/types`-based check.
Recommendation: (a), scoped to one fixture file. Lector's call.

## 8. Resolved review questions

Lector's design r1 answered every open question; they are recorded here as
decisions:

1. **`dao.T` / `dao.C` stay** — the brevity is the point at a declaration site,
   and the type-parameter `C` lives in a different scope. No unquoted variant is
   needed either: §2.4's quoting is a provable no-op for the whole evidence base
   and `SQL()` covers the mixed-case escape.
2. **`Lit(any)` is gone**, replaced by the closed `Str`/`Int` set that refuses
   what it cannot render portably (§2.3, alternative 6). Panic-at-declaration
   stays: it is the earliest possible failure for a compile-time constant.
3. **§2.6's write-safety check ships with this ADR**, but only because write
   identity is now represented (§2.2) — the check is coherent *after* resolution
   and incoherent before it.
4. **`Coalesce` is generic over a closed union** (rev 2–3, Johno's calls:
   *"let's keep it simple, but do keep dao.Str, might have use for that"* and
   *"Coalesce should only accept string/int anyways"*). r1's substance is intact
   — the renderable literals are still exactly `Str` and `Int` with their
   refusal rules, and there is still no public `Lit(any)`. The sketched
   `Coalesce(x, "")` reads as written, and "string or int only" is now enforced
   by the **type system** rather than a declaration panic. `Str`/`Int` stay
   exported for explicit use and future composition points.
5. **The §2.7 exclusions stand** — `Lower`/`Upper`/`Concat`/`Cast`, aggregates,
   and the `SortMap`/search-op variants remain deferred.

## Review history

- **r1 (2026-08-21, lector — `change_requested`, folded in this revision).**
  **Must-fix 1 was a fatal defect, not a nit:** rev 0's `Expr` was
  `func(Dialect) string`, so the rendered *quoted* projection reached
  `writeCol()` and `INSERT`/`UPDATE` quoted it a second time — every writable
  `T`/`C` field would have emitted invalid DML, and rev 0's own §2.6 check would
  have rejected the ADR's north-star examples at `New`. Verified against the
  real code before rewriting: `Column "artist"."name"` yields `writeCol "name"`
  and `QuoteIdent` then emits a triple-quoted identifier. `Expr` is now a struct
  carrying the raw write identity, an explicit `WriteColumn` still wins, and
  compositions carry none (§2.1, §2.2), with read *and* write equivalence in
  acceptance criterion 2. **Must-fix 2:** `Fields(m)` aliases the caller's map
  and `New` stores that reference, so rev 0's in-place resolution would have
  mutated a package-level `var` — a second `New` would then see both `Column`
  and `Expr` (tripping the panic) or inherit the first dialect. Resolution now
  clones (§2.2), with criterion 3b proving the source map is untouched across
  two dialects. **Must-fix 3:** `Lit(any)` removed — quote-doubling is not a
  portable literal contract (MySQL's `NO_BACKSLASH_ESCAPES` and charset), and
  `any` left float/non-finite behavior undefined; replaced by the closed
  `Str`/`Int` set that refuses what it cannot render identically everywhere
  (§2.3, alternative 6). **Should-fix 1:** same-key `OptionalJoin` /
  `OptionalJoinExpr` precedence is now specified as later-wins with the opposite
  representation deleted, tested in both orders (§2.5, criterion 7).
  **Should-fix 2:** `T` accepts schema-qualified table constants and renders the
  table part through `quoteTable`, inheriting ADR-0013's fallback (§2.4,
  criterion 1).
  Review doc: `$KB_ROOT/agents/lector/reviews/2026-08-21-golib-dao-0016-declarative-column-expressions-review.md`
- **rev 2 (2026-08-21, Johno).** Literal sugar restored: `Coalesce(e, alt any)`
  instead of `Coalesce(e, alt Expr)`, on his direction — *"yeah, let's keep it
  simple, but do keep dao.Str, might have use for that"*. This is a deliberate
  narrowing of r1's must-fix 3 to its actual concern: the portability rule is
  unchanged (a literal is still renderable only through `Str`/`Int`, refusal
  rules included, and a float/bool/struct still panics at declaration), and no
  public `Lit(any)` returns — the routing is one unexported helper behind a
  single parameter. `Str` and `Int` remain exported for explicit use.
