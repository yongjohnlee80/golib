# ADR-0010 — `golib/dao`: Partial-Update Rules & Per-Column Clearability

- **Status:** Proposed (revision 2 — lector r1 amendments applied, see §7)
- **Date:** 2026-07-04
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive to ADR-0002/0003/0006)
- **Related:** ADR-0002 (interfaces — `Set`/`SetMap`/`Clear`), ADR-0003 (DAL
  impl — staging/build), ADR-0006 (factory/builder — `Field` declarations),
  ADR-0009 (query-time options & hooks — the seam this layer coexists with),
  `golib-partial-0001` (the `golib/partial` package — the primary producer of
  rules; designed together with this ADR)

> **Self-containment contract.** Like ADR-0001…0009, this document is written
> so an implementer with no prior context can build the feature: it restates
> the context, gives concrete Go signatures and representative bodies, names
> the files to create/modify, and lists acceptance criteria.

---

## 1. Context

### 1.1 The gap

A PATCH endpoint needs to express, per field, one of three intents:

1. **Write** — the request carried a value; set the column to it.
2. **Skip** — the request omitted the field; leave the column alone.
3. **Clear** — the request asked for the field to be unset; write the
   column's cleared state (usually SQL `NULL`).

`golib/dao` today can express only the first. The staging surface is
`Set`/`SetMap`/`Clear` (`dao/query_dao.go`), where `Clear(field)` is literally
`Set(field, nil)` — an unconditional NULL write with no notion of whether the
column *may* be cleared, and no way to say "skip". `DefaultValues`
(`dao/options.go`) is build-time only. The 2026-07-04 audit rated this the
package's #1 capability gap (§2.2.1): every PATCH-shaped update must be
hand-rolled per call site — build a values map, conditionally add entries,
remember which nils mean "clear" and which mean "wasn't sent".

The prior art this package rewrote (Label Manager's DAL) had full machinery
here, and its shape is both the proof of need and the catalogue of mistakes
to avoid:

- **Value-time sentinels.** LM passed `postgres.OmmitedValue{}` /
  `postgres.ClearedValue{Value: …}` *as values* inside `map[string]any` rules
  maps; `Updater.Set` type-switched on them at write time
  (`pkg/postgres/updater.go:114-136`). It worked — overrides resolved
  order-independently against staged data — but the semantics live in untyped
  magic values a reader cannot distinguish from data, and the misspelled
  `OmmitedValue` is frozen into 50+ files.
- **Per-column clearability authority.** Whether a `$clear` may target a
  column was decided by an explicit per-entity map — e.g.
  `releaseClearableColumns` (`internal/dao/release-postgres.go:1063-1069`):
  present-with-nil ⇒ clears to SQL NULL; present-with-sentinel ⇒ a **NOT
  NULL** column "clears" to a sentinel (`release_date` →
  `lib.DateSentinel`); absent ⇒ not clearable. Nilability of the Go field
  deliberately had **no** bearing.
- **The NOT-NULL-clear downgrade.** `dao.ResolveClears`
  (`internal/dao/override-rules.go:44-56`) resolved each requested clear
  against that authority: clearable → write the declared clear-value;
  not clearable → *downgrade to an omit* so the clear silently no-ops. Wire
  semantics, deliberately forgiving: a typo, an unsupported field, or a NOT
  NULL column produces no write rather than a 500 — mirroring how partial
  payloads already ignore unknown keys on the set path.
- **The cost center.** Field→column translation and the clearability maps
  were hand-written per entity: ~50 `SetOverrideRules` switch methods, ~10
  clearable-column maps, per-entity payload factories. That is exactly the
  threefold-restatement disease golib's `Field` map already cured for
  reads/writes — the rules layer must come equally free from the schema.

golib/dao must gain the capability without importing the mistakes: typed
rules instead of sentinel values, clearability declared once on the existing
`Field`, and zero per-entity translation code.

### 1.2 Where writes already funnel

Every write verb stages into one structure and resolves it in one place:
`Set`/`SetMap`/`Clear` put `writeCol → value` into `writeState.set`
(an `orderedSet`, `dao/scanner.go`), and `stagedSet()`
(`dao/query_dao.go`) merges `DefaultValues` then per-call staging before
`buildInsert`/`buildUpdate`/`buildUpsert` render it. A rules layer therefore
needs exactly one new resolution step inside `stagedSet()` — no verb bodies
change shape. (This is the write-phase mirror of ADR-0009's observation that
all statements funnel through one pipeline.)

Two properties of the existing engine are load-bearing here and must be
preserved:

- **Empty update is a no-op, not an error.** `Update()` returns `nil` when
  nothing is staged — a partial payload can legitimately reduce to zero set
  columns (LM learned this the hard way; its `Updater.Update` grew the same
  guard).
- **Unknown/read-only fields fail loudly on the developer surface.**
  `Set` stages `ErrUnknownField` / `ErrReadOnlyField` immediately
  (`dao/query_dao.go`). The rules surface deliberately diverges for
  wire-derived maps — see §2.4.

### 1.3 Goals

- **G1 — Typed rules.** Write/Skip/Clear are a closed, typed vocabulary — no
  sentinel values hiding in `any`, nothing a reader can confuse with data.
- **G2 — Clearability is a declared, per-column authority.** On the `Field`,
  next to everything else the column declares. NOT NULL columns can declare a
  clear sentinel. Go-type nilability never implies clearability.
- **G3 — Wire-safe by default.** A clear against a non-clearable column
  downgrades to a skip (LM's proven semantics); a strict mode turns it into
  an error for teams that want loud failures.
- **G4 — Order-independent resolution.** Rules are authoritative for their
  field regardless of the order of `Set`/`SetMap`/`SetRules` calls — LM's
  override-reconciliation lesson, kept without its re-run machinery.
- **G5 — Zero per-entity code.** Field→column translation comes from the
  `Schema`'s existing `Field` declarations; a PATCH handler wires a rules map
  (or a `golib/partial` patch, via the adapter contract in §2.6) straight
  into the DAO.
- **G6 — Zero cost / zero break when unused.** No rules staged → identical
  behavior, no allocations; every existing call site compiles and behaves
  unchanged.

### 1.4 Non-goals

- **Not the partial-payload package.** Wire parsing, presence tracking,
  `$clear` channels, bind-time validation, and mutators are
  `golib/partial` (`golib-partial-0001`). dao consumes resolved rules and
  never sees JSON.
- **No audit/diffing.** Change tracking is a consumer concern (same posture
  as `golib-partial-0001`).
- **No per-row rules for `BatchWriter`.** ADR-0009 §2.6 deferred
  "batch-shaping" here; the analysis concludes: PATCH semantics target one
  entity by predicate, and batch's model-extractor path (`Field.Value`) has
  no presence information to honor. Bulk partial updates are a predicate'd
  `Update`, or a loop. Revisit only with a concrete consumer.
- **No DB-side `DEFAULT` rewriting.** `Skip` omits the column; whatever the
  database does for omitted columns (INSERT defaults) stands.

---

## 2. Decision

### 2.1 The rules vocabulary (`dao/rules.go`, new)

```go
package dao

// Rule is one field's disposition in a partial write: write a value, skip
// the field, or clear it. The zero Rule is Skip — a rules map that doesn't
// mention a field and one that maps it to the zero Rule mean the same thing.
// Construct via Write/Skip/Clear; the kind is not exported so the vocabulary
// stays closed (no sentinel-value pattern — the LM lesson, §1.1).
type Rule struct {
	kind  ruleKind
	value any
}

type ruleKind uint8

const (
	ruleSkip ruleKind = iota // zero value: leave the column alone
	ruleWrite
	ruleClear
)

// Write returns a Rule that stages v for the field (same write path as Set).
func Write(v any) Rule { return Rule{kind: ruleWrite, value: v} }

// Skip returns a Rule that leaves the field alone. It is authoritative: a
// Skip removes the field from the staged write even if Set/SetMap/DefaultValues
// staged a value for it (see §2.3).
func Skip() Rule { return Rule{} }

// Clear returns a Rule that requests the field's cleared state. What that
// means is decided by the Field's clearability declaration (§2.2): the
// declared ClearValue (default SQL NULL) for a Clearable field, a downgrade
// to Skip — or ErrNotClearable under StrictClears — otherwise.
func Clear() Rule { return Rule{kind: ruleClear} }
```

The `DAO` interface (ADR-0002, `dao/dao.go`) gains one intent method,
symmetric with `SetMap`:

```go
// SetRules stages a partial-write disposition per field: Write stages a
// value, Skip removes any staged value for the field, Clear stages the
// field's declared cleared state (§2.2). Rules are authoritative for their
// field over Set/SetMap/DefaultValues regardless of call order; across
// multiple SetRules calls the last rule per field wins.
//
// SetRules is the WIRE-FACING write surface: keys that don't resolve to a
// writable field (unknown, or ReadOnly) are skipped silently, because a
// rules map is typically derived from request data whose extra keys are
// normal (see the trust-boundary note). Set/SetMap keep their loud
// ErrUnknownField/ErrReadOnlyField behavior for developer-authored writes.
SetRules(rules map[C]Rule) DAO[R, C, ID]
```

### 2.2 Clearability: declared on the `Field` (`dao/field.go`)

`Field[R]` (ADR-0006) grows the per-column authority:

```go
type Field[R any] struct {
	// ... Column, Scan, Value, Join, ReadOnly, WriteColumn as today ...

	// Clearable declares that a rules-driven Clear (SetRules) may target this
	// column. It is a deliberate per-column decision — never inferred from
	// the Go field's nilability (a nilable column absent this flag is not
	// clearable; a NOT NULL column can be Clearable via ClearValue).
	Clearable bool

	// ClearValue is what a clear writes when Clearable is true: nil (the
	// default) writes SQL NULL; a non-nil value is the cleared-state sentinel
	// for a NOT NULL column (e.g. a date sentinel). Setting ClearValue with
	// Clearable false is a declaration error rejected by dao.New.
	ClearValue any
}
```

`dao.New` (ADR-0006's build step) validates the declaration: a field with
`ClearValue != nil && !Clearable` fails schema construction with a config
error, alongside the existing build-time checks. A misdeclared schema should
never boot.

**Resolution semantics for a `Clear()` rule:**

| Field declares | `Clear()` resolves to |
|---|---|
| `Clearable: true` (ClearValue nil) | stage SQL `NULL` |
| `Clearable: true, ClearValue: S` | stage `S` (NOT-NULL sentinel) |
| not clearable (default) | **downgrade to Skip** — the column is left alone |
| not clearable + `StrictClears()` | an **error rule** for the field; the verb returns `ErrNotClearable` and executes nothing — **iff it is still the field's final rule** (§2.3) |

The downgrade default is LM's proven wire semantics (`ResolveClears`,
§1.1): a PATCH listing a field the entity doesn't clear produces no write and
no error. Request-shape validation ("is clearing this field even a thing?")
belongs at bind time in `golib/partial`, not in the engine. The strict mode
is a **schema build option** for teams that prefer loud failure — its error
travels *with the field's rule*, not through the DAO's sticky first-error
path, so a later rule for the same field replaces it (§2.3):

```go
// StrictClears makes a rules-driven Clear on a non-Clearable field an error
// (ErrNotClearable) instead of a silent skip. Schema-wide, build-time. The
// error is carried on the field's resolved rule and surfaces at the write
// verb only when it is the field's final rule — a later Write/Skip/valid
// Clear for the same field replaces it (last-rule-wins, §2.3).
func StrictClears[R any, C ~string, K ~string, ID any]() Option[R, C, K, ID]
```

with the matching sentinel in `dao/errors.go`:

```go
// ErrNotClearable is returned by a write verb when, under StrictClears, a
// field's FINAL rule is a Clear targeting a non-Clearable field. Wrapped
// with the field name; test with errors.Is.
ErrNotClearable = errors.New("dao: field is not clearable")
```

### 2.3 Resolution: one step in `stagedSet()`, order-independent

`SetRules` resolves each entry **immediately** (the schema is at hand):
field → `writeCol`, `Clear()` → the declared clear value / downgrade /
strict-error rule. What it stores in `writeState` is a small resolved map,
separate from the `Set` staging:

```go
type writeState struct {
	set   orderedSet
	rules map[string]resolvedRule // writeCol → resolved disposition
}

type resolvedRule struct {
	kind  ruleKind // ruleWrite | ruleSkip | ruleClear (clear: value resolved)
	value any
	err   error // StrictClears violation; non-nil only with kind ruleSkip
}
```

A strict-clear violation is **not** routed through the DAO's sticky
first-error path (`queryDAO.fail`, first-error-wins): that would make the
outcome depend on *when* the offending rule was staged, contradicting this
section's guarantees. Instead it is stored as that field's resolved rule
(`resolvedRule{kind: ruleSkip, err: fmt.Errorf("%w: %v", ErrNotClearable,
field)}`); like any other rule it is **replaced** by a later `SetRules`
entry for the same field, and only a violation that survives as the field's
final rule errors the write.

`stagedSet()` applies the layers in fixed precedence — schema defaults, then
per-call `Set`/`SetMap`/`Clear`, then rules — and reports the first
surviving strict violation (by column order, deterministically):

```go
func (d *queryDAO[R, C, K, ID]) stagedSet() (orderedSet, error) {
	var set orderedSet
	for c, v := range d.schema.defaultVals {
		set.put(c, v)
	}
	for c, v := range d.w.set.m {
		set.put(c, v)
	}
	for _, col := range sortedRuleCols(d.w.rules) {
		r := d.w.rules[col]
		if r.err != nil {
			return orderedSet{}, r.err // final rule is a strict-clear violation
		}
		switch r.kind {
		case ruleWrite, ruleClear: // clear carries its resolved value
			set.put(col, r.value)
		case ruleSkip:
			set.del(col) // authoritative: removes staged/default values
		}
	}
	return set, nil
}
```

The write verbs (`Insert`/`Update`/`Upsert`) consume the error before their
existing empty-set guards; nothing executes when it is non-nil.

Because rules live apart from staged values and are applied last, resolution
is order-independent by construction: `Set(f, v).SetRules(m)` and
`SetRules(m).Set(f, v)` produce identical SQL when `m` carries a rule for
`f` (the rule wins) — and under `StrictClears`,
`SetRules({f: Clear()}).SetRules({f: Skip()})` is exactly `Skip` (no error),
while `SetRules({f: Clear()})` alone errors. This keeps LM's override-wins
guarantee (`SetOverrides`/`Set` reconciliation,
`pkg/postgres/updater.go:96-136`) without its re-run machinery. `orderedSet`
gains the trivial `del`.

Two existing behaviors compose, unchanged:

- **All-skip updates no-op.** Rules that reduce the staged set to empty hit
  `Update()`'s existing `set.empty() → return nil` guard — the empty-PATCH
  case costs nothing and errors nothing.
- **Insert/Upsert accept rules too.** `Skip` omits the column (the DB
  default applies); `Clear` stages the resolved clear value; `Write` stages
  the value. `Insert` with an all-skip rule set still returns
  `ErrNothingToInsert` — an INSERT that writes nothing is a caller bug,
  exactly as today.

### 2.4 The trust boundary: `SetRules` is lenient, `Set` stays loud

`Set`/`SetMap` keep erroring on unknown and read-only fields: their keys are
developer-authored enum constants, and a miss is a code bug that must fail
fast (`ErrUnknownField`/`ErrReadOnlyField`, unchanged).

`SetRules` **skips** unknown and read-only keys silently. Its maps are
wire-derived — built from a patch whose field names crossed an HTTP boundary
(cast into `C` by the adapter, §2.6). A patch echoing a computed/read-only
field, or carrying a field that simply isn't a column of this entity, is
normal traffic, not a server fault; rejecting request-shape problems is
bind-time validation in `golib/partial`, against the model type — not the
engine's job. This mirrors LM's "partial payloads ignore unknown keys on the
set path" and is documented loudly on the method.

(A developer who wants strict keys with rules semantics composes them from
enum constants — where unknowns are impossible by construction — and relies
on `StrictClears` for clearability strictness.)

### 2.5 Developer `Clear()` vs rules `Clear()`: the same trust split

The existing fluent `Clear(field C)` is **developer intent in trusted
code** — it keeps its direct semantics and does not consult `Clearable`:

- Field declares `ClearValue` → `Clear` now stages that sentinel (the one
  behavior refinement: a declared NOT-NULL clear sentinel is honored
  everywhere, not just via rules).
- Otherwise → stages `nil` (SQL NULL), byte-identical to today. Clearing a
  NOT NULL column without a declared sentinel fails at the database with
  `ErrNotNull` — as it always has.

Rules-driven `Clear()` is **request-derived intent** and always goes through
the clearability authority (§2.2). Same split LM ran: hand-written
`ClearUPC()` called `Set(col, nil)` directly, while wire `$clear` resolved
via `releaseClearableColumns`. Since no existing schema declares `Clearable`
or `ClearValue`, no existing `Clear()` call changes behavior.

### 2.6 The `golib/partial` seam: a contract, not a coupling

dao does not import `golib/partial`; partial (or its adapter subpackage)
imports dao. The contract this ADR pins, consumed by `golib-partial-0001`:

1. `partial.Patch[T]` exposes a rules projection — per effective field name,
   one of Write(value)/Skip/Clear — derived from presence: absent ⇒ Skip,
   `null`/`$clear` ⇒ Clear (per the patch's clear policy), value ⇒ Write.
2. A generic adapter converts that projection to `map[C]dao.Rule` by casting
   names (`C(name)`) and mapping kinds 1:1, then calls `SetRules`. Shape:

```go
// in golib/partial (or partial/pdao) — golib-partial-0001 owns the final home
func ApplyRules[R any, C ~string, ID any, T any](
	d dao.DAO[R, C, ID], p *Patch[T],
) dao.DAO[R, C, ID] {
	rules := make(map[C]dao.Rule, ...)
	for name, r := range p.Rules() {
		rules[C(name)] = convert(r) // Write/Skip/Clear, 1:1
	}
	return d.SetRules(rules)
}
```

3. Name alignment is a **declaration convention, not translation code**: an
   entity's field-enum values are its wire field names (golib entities own
   both sides of that string). Where they must diverge, the adapter accepts
   a rename hook — `golib-partial-0001`'s decision. Either way, `SetRules`'s
   lenient key handling (§2.4) makes stray wire fields safe.

This is what collapses LM's per-entity cost center (~50 switches, ~10
clearable maps, payload factories) to zero: field→column comes from
`Fields(...)`, clearability from the `Field` declaration, translation from
a type cast.

### 2.7 North-star usage

```go
// ---- declaration: clearability lives with everything else the column says ----
var releases = dao.New[*model.Release, RelField, RelSort, string](conn,
	dao.Table("release"),
	dao.ID[...](RelID),
	dao.Fields(map[RelField]dao.Field[*model.Release]{
		RelID:       {Column: "id", ReadOnly: true, Scan: ...},
		RelTitle:    {Column: "title", Scan: ..., Value: ...},        // not clearable
		RelUPC:      {Column: "upc", Clearable: true, Scan: ...},     // clears → NULL
		RelDate:     {Column: "release_date",                        // NOT NULL:
			Clearable: true, ClearValue: model.DateSentinel,          // clears → sentinel
			Scan: ..., Value: ...},
	}),
)

// ---- a PATCH handler, hand-rolled rules (no partial package needed) ---------
err := releases.OnCtx(ctx).
	With(RelID, id).
	SetRules(map[RelField]dao.Rule{
		RelTitle: dao.Write(req.Title), // value present → write
		RelUPC:   dao.Clear(),          // → upc = NULL
		RelDate:  dao.Clear(),          // → release_date = DateSentinel
		// fields not mentioned (or dao.Skip()) are left alone
	}).
	Update()

// ---- the same handler via golib/partial: zero per-entity code ---------------
patch, err := partial.Bind[model.Release](r.Body) // presence + validation
// ... server-side strips/mutations on patch ...
err = partial.ApplyRules(releases.OnCtx(ctx).With(RelID, id), patch).Update()
```

Note what is absent: no per-entity rules method, no clearable-columns map
beside the schema, no sentinel values in `any`, and no change to any
existing call site.

---

## 3. Consequences

**Positive.** PATCH becomes expressible in one declaration-driven call —
the audit's #1 dao gap closes. Clearability is schema data next to the
column it governs, honest about NOT NULL (sentinels declared, not
discovered in production). The rules vocabulary is typed and closed; the
LM sentinel-value and per-entity-switch failure modes are structurally
impossible. `golib-partial-0001` gets a stable, minimal target.

**Negative / costs.** `DAO` (an interface golib owns but consumers may have
mocked in tests) grows one method — pre-1.0, accepted, called out in the
changelog; consumer *code* is unaffected. The lenient/loud split between
`SetRules` and `SetMap` is two postures on one surface — mitigated by
documenting both methods against each other (§2.4's rationale verbatim).
`Field` gains two members whose misuse (`ClearValue` without `Clearable`)
is caught at build, not compile.

**Migration.** None: no existing schema declares clearability, no existing
call uses rules; behavior without them is byte-identical (acceptance
criterion 8).

---

## 4. Alternatives considered

- **LM's value-time sentinels** (`OmittedValue`/`ClearedValue` passed as map
  values, type-switched at write). Proven, but the semantics hide in untyped
  values — invisible to the reader and the type system, and the pattern is
  what demanded per-entity translation methods. Rejected for a closed, typed
  `Rule`.
- **Per-entity rules methods** (LM's `SetOverrideRules` switches + clearable
  maps). The measured cost center (§1.1). Rejected: the `Schema` already owns
  field→column and now owns clearability.
- **Clearability inferred from Go nilability** (pointer field ⇒ clearable).
  Explicitly rejected by LM's own docs after real bugs — nilability is a
  scanning concern, clearing is a business decision; NOT NULL sentinel
  columns break the inference in both directions. Kept as a declared flag.
- **Hooks as the rules channel** (a `BeforeBuild` hook staging via
  ADR-0009's `Stager.SetColumn`). Rules are per-call *payload data*, not
  cross-cutting *policy* — a hook would smuggle request data into a policy
  seam and lose the typed field enum. Rejected as the primary channel; hooks
  still observe/augment the statement after rules resolve (the two layers
  compose: rules shape the SET, a tenant hook still scopes the WHERE).
- **Strict-by-default on unclearable clears.** Honest but hostile at the
  boundary that actually produces clears (the wire): LM's downgrade exists
  because PATCH traffic legitimately mentions fields an entity doesn't
  clear. Default stays downgrade; `StrictClears` is the opt-in.
- **`$clear`/null parsing in dao.** Would drag wire semantics, JSON, and
  presence into the engine and couple it to one payload format. Rejected:
  dao consumes resolved rules; `golib/partial` owns the wire
  (`golib-partial-0001`).
- **Per-row rules on `BatchWriter`.** Deferred non-goal (§1.4): no presence
  data on the extractor path, no concrete consumer; ADR-0009's deferral is
  resolved by declaring it out of scope rather than designing speculatively.

---

## 5. Acceptance criteria

1. **Rules land in SQL.** Against the fake `DataConn`: `Write(v)` binds `v`;
   `Skip()` omits the column; `Clear()` on a `Clearable` field binds SQL
   NULL; `Clear()` on `Clearable+ClearValue` binds the sentinel — one UPDATE
   asserting all four columns' rendered SET and args.
2. **Order independence.** `Set(f,v)` before `SetRules` and after produce
   identical SQL/args when the rules map carries a rule for `f`; a rule also
   overrides a `DefaultValues` entry; last `SetRules` call per field wins.
3. **Downgrade semantics.** `Clear()` on a non-clearable field drops the
   column; an update whose staged set becomes empty returns `nil` and
   executes nothing (fake records no call).
4. **Strict mode, last-rule-wins.** On a `StrictClears()` schema: a final
   `Clear()` on a non-clearable field returns `ErrNotClearable`
   (`errors.Is`, message names the field) and executes nothing; the
   replacement cases hold — `SetRules({f: Clear()}).SetRules({f: Skip()}).
   Update()` executes nothing and returns `nil`, and
   `SetRules({f: Clear()}).SetRules({f: Write(v)}).Update()` writes `v` —
   proving the violation is not sticky.
5. **Trust split on keys.** `SetRules` with unknown and `ReadOnly` keys
   skips them without error while honoring valid entries in the same map;
   `Set`/`SetMap` still stage `ErrUnknownField`/`ErrReadOnlyField`.
6. **Declaration validation.** `dao.New` with `ClearValue` set on a
   non-`Clearable` field fails schema construction with a config error
   naming the field.
7. **`Clear()` refinement.** Fluent `Clear(field)` stages the declared
   `ClearValue` when present, `nil` otherwise; for fields with no
   clearability declaration its emitted SQL is byte-identical to pre-ADR.
8. **No-rules baseline.** With no `SetRules` call, no allocations are added
   to the write path (benchmark vs pre-ADR) and the full pre-ADR test suite
   passes unmodified. Insert/Upsert honor rules per §2.3 (Skip omits →
   DB default; all-skip Insert returns `ErrNothingToInsert`).

## 6. File plan

| File | Change |
|---|---|
| `dao/rules.go` | new — `Rule`, `ruleKind`, `Write`, `Skip`, `Clear`, `resolvedRule` (with strict-violation `err` carrier) |
| `dao/dao.go` | `SetRules(map[C]Rule)` on the `DAO` interface |
| `dao/field.go` | `Field.Clearable`, `Field.ClearValue` |
| `dao/options.go` | `StrictClears()` build-time option |
| `dao/errors.go` | `ErrNotClearable` |
| `dao/schema.go` | build-time validation: `ClearValue` requires `Clearable` |
| `dao/query_dao.go` | `SetRules` impl (immediate resolution); `writeState.rules`; `stagedSet() (orderedSet, error)` rules step + strict-violation surfacing, write verbs consume the error; `Clear()` honors `ClearValue` |
| `dao/scanner.go` | `orderedSet.del` |
| `dao/rules_test.go` | new — acceptance criteria 1–8 against the fake `DataConn` |

---

## 7. Review history

- **r1 (2026-07-04, lector): `change_requested`** — review doc:
  `agents/lector/reviews/2026-07-04-golib-dao-adr-0010-review.md`.
  Amendments applied in revision 2:
  - **must-fix #1**: strict-clear errors no longer stage through the DAO's
    sticky first-error path (which would have made
    `SetRules({f: Clear()}).SetRules({f: Skip()})` order-dependent,
    contradicting the last-rule-wins guarantee). The violation now travels
    on the field's `resolvedRule` (`err` carrier), is replaced by any later
    rule for the same field, and surfaces via
    `stagedSet() (orderedSet, error)` only when it survives as the field's
    final rule; write verbs consume the error before their empty-set guards
    (§2.2 table, §2.3, criterion 4 replacement cases, file plan).
