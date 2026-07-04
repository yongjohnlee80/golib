# ADR-0001 — `golib/partial`: Generic Partial-Payload Package (`Patch[T]`)

- **Status:** Proposed
- **Date:** 2026-07-04
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (new package; first ADR of the `partial` dossier)
- **Related:** dao ADR-0010 (partial-update rules & per-column clearability —
  the DAO seam `Rules()`/`ApplyRules` target, designed together with this
  ADR), dao ADR-0002 (the `DAO[R,C,ID]` fluent surface), dao ADR-0009
  (query-time hooks — orthogonal; a patch shapes the SET, hooks shape
  policy), golib conventions (options idiom, loud developer surfaces)

> **Self-containment contract.** Like the dao dossier (ADR-0001…0010), this
> document is written so an implementer with no prior context can build the
> package: it restates the context, gives concrete Go signatures and
> representative bodies, names the files to create, and lists acceptance
> criteria.

---

## 1. Context

### 1.1 The problem

An HTTP PATCH body is a *three-state* document: for each field of the target
model, the request either **carries a value**, **omits the field**, or **asks
for it to be unset**. Go's `encoding/json` collapses the last two — decoding
into a struct leaves both an omitted field and an explicit `null` at the zero
value — so a plain `json.Unmarshal` into the model cannot drive a partial
update. Every service needs presence tracked *out of band* from the typed
struct.

golib now has the DAO side of this story: dao ADR-0010's `SetRules` consumes
a per-field Write/Skip/Clear disposition, with clearability declared on the
`Field`. What's missing is the wire side — the thing that parses a PATCH
body once, tracks presence, validates at bind time, survives server-side
mutation, and emits that disposition. That is `golib/partial`.

### 1.2 What LM taught us

The prior art (Label Manager's `internal/partial`, ~900 lines) is the
proof of need and the catalogue of costs. Its shape: `Payload[T]` embeds a
mutable `audit.DataMap` (`map[string]any`), presence = key-in-map, wire
`null` deliberately means *absent* (`payload.go:139-142` — encoding/json
leaves non-pointer fields untouched on null, so a present-but-null entry is
indistinguishable from an omitted one at the typed layer), and clearing to
SQL NULL rides a reserved `$clear` array (`clearable.go:17`) popped out of
the map at bind time.

**Keep (proven semantics):**

- **Presence out-of-band from the typed struct** — the payload is the unit
  passed between layers; the typed model is derived on demand.
- **An explicit clear channel as an unambiguous option** — `$clear` cannot
  be confused with a zero value, and the value-AND-clear ambiguity is
  rejected at bind time (`clearable.go:61-79`).
- **Bind-time validation, not mutation-time** — the route's `Remove`/`Set`/
  `Only` mutations run after binding and must not retroactively invalidate
  an already-accepted request (`payload.go:287-295`).
- **Server-side mutators with last-mutation-wins-over-clear** — `Set` drops
  the key from the clear list (a clear entry must not beat the route's
  injected value with a NULL write, `payload.go:81-94`); `Remove` strips
  both channels (`payload.go:117-128`); `Only` intersects both.
- **Emptiness covers both channels** — a clear-only payload is not empty
  (`clearable.go:94-100`).

**Fix (measured costs):**

- **Double/triple parse.** Bytes → `map[string]any` at bind; map → bytes →
  `T` again at `Data()` (`payload.go:183-201`); mutations invalidate a
  byte cache that then re-marshals the map. Three traversals of the same
  data, two of them through `any`.
- **Presence keyed by Go field name.** `Attributes()` reflects struct field
  *names* (`payload.go:204-234`), so a `json:"artist_title"` tag silently
  breaks presence detection — the map key is the wire name, the attribute
  key is the Go name, and they only agree by convention.
- **Uncached reflection per call.** `attributesOf[T]()` walks the type on
  every `OmittedFields`/`ExtractMap`/`Contains` call.
- **Anonymous-pointer-embed cliff.** Pointer embeds are silently skipped by
  the reflect walk (`payload.go:216-220`) — fields vanish from presence
  tracking with no error.
- **Public mutable state.** The embedded `DataMap` is exported and mutable;
  correctness depends on callers remembering `ClearCache()` after direct
  writes (`payload.go:19-21`).

**Drop (couplings and the cost center):**

- Dependencies on `entity-audit`, `errs.Validation`, `nilable`,
  `go-collections` — golib packages are dependency-free by convention.
- Audit diffing inside the payload (`CompareFrom`) — a consumer concern.
- The per-entity translation layer this fed: ~50 `SetOverrideRules`
  switches, ~10 clearable-column maps, per-entity payload factories. dao
  ADR-0010 §2.6 already collapsed the DAO side to zero per-entity code;
  this package must feed it with zero per-entity code of its own.

### 1.3 Goals

- **G1 — Parse once.** One traversal of the body: per-field raw slots keyed
  by the *effective JSON name*, plus one deferred typed decode. No
  `map[string]any` round-trips.
- **G2 — Three-state fidelity.** Absent / null / value are all
  caller-distinguishable; what "null" *means* is a declared policy (§2.3),
  not an accident of `encoding/json`.
- **G3 — One name space.** Presence, mutation, rules, and validation all key
  by the effective JSON name from a per-`T` cached plan; tag divergence
  cannot break detection. Pointer embeds fail loudly at plan build.
- **G4 — Package-owned typed errors.** `partial.ValidationError` with field
  path + reason, `errors.As`-able, produced at bind time; no app error
  imports.
- **G5 — LM's mutation semantics, hardened.** `Set`/`Clear`/`Remove`/`Only`
  with last-mutation-wins-over-clear; unknown field names are loud (the
  mutator surface is developer code, not wire data).
- **G6 — The DAO seam is a projection, not a coupling.** `Rules()` emits the
  Write/Skip/Clear disposition; `ApplyRules` adapts it onto dao ADR-0010's
  `SetRules` with zero per-entity code. dao never imports partial.

### 1.4 Non-goals

- **No audit/diffing** — expose the presence/value projection; diffing is a
  consumer concern.
- **No recursive deep-merge.** Nested objects are whole-object replace;
  `Patch` composition (§2.7) covers nested partial semantics. Deep-merge is
  explicitly out of scope for v1.
- **No HTTP framework surface.** `Bind` takes `[]byte`/`io.Reader`;
  content-type negotiation, body limits, and 4xx rendering belong to the
  server layer.
- **No `encoding/json/v2` (yet).** Evaluated per the plan: as of Go 1.25 it
  is `GOEXPERIMENT`-gated — a library cannot impose an experiment on its
  consumers. The design isolates decoding in two funnel points (§2.2 bind,
  §2.5 per-field decode) so a v2 swap is a two-function change; the raw-slot
  model is exactly v2-friendly.

---

## 2. Decision

### 2.1 The carrier: `Patch[T]` (`partial/patch.go`, new)

```go
package partial

// Patch is a presence-aware partial payload for the model type T. It is the
// unit passed between bind, server-side mutation, and the DAO adapter; the
// typed T is derived on demand (Data) from one underlying parse.
//
// The zero Patch is empty and usable (a server-constructed patch starts
// zero and is populated via Set/Clear). A Patch is not safe for concurrent
// use.
type Patch[T any] struct {
	fields map[string]json.RawMessage // canonical name → raw value bytes
	clear  map[string]struct{}        // canonical names marked cleared
	mode   ClearMode

	obj   T    // cached Data() decode
	objOK bool
	err   error // sticky first mutator error (unknown field, marshal failure)
}

// State is one field's disposition in the patch.
type State uint8

const (
	Absent  State = iota // not mentioned: skip on write
	Present              // carries a value: write it
	Cleared              // marked cleared: write the cleared state
)
```

The two channels are disjoint by invariant: a name is never in both `fields`
and `clear` (bind validates, mutators maintain). Presence is keyed by
**canonical effective JSON names** resolved through the per-`T` plan (§2.4)
— never by Go field names (the LM tag-divergence bug is structurally
impossible) and never by raw wire spelling (case-variant keys are
normalized at bind, mirroring `encoding/json`'s own match rules).

Read surface:

```go
// State reports the field's disposition. Unknown names report Absent.
func (p *Patch[T]) State(field string) State

// Present / Cleared are convenience predicates over State.
func (p *Patch[T]) Present(field string) bool
func (p *Patch[T]) Cleared(field string) bool

// Contains reports whether every named field is Present.
func (p *Patch[T]) Contains(fields ...string) bool

// Fields returns the canonical names carried by the patch (Present and
// Cleared), sorted — the enumerable view.
func (p *Patch[T]) Fields() []string

// Empty reports whether the patch carries nothing to apply — no values AND
// no clears (the LM emptiness lesson: a clear-only patch is not empty).
func (p *Patch[T]) Empty() bool

// Err returns the sticky first mutator error (nil when clean). Data, Rules,
// and ApplyRules also surface it.
func (p *Patch[T]) Err() error
```

### 2.2 Binding: one parse, bind-time validation (`partial/bind.go`, new)

```go
// Bind decodes a PATCH body into a Patch[T]: one pass into per-field raw
// slots, wire keys normalized to canonical names via the type plan, the
// clear channel resolved per the ClearMode, and the full typed decode run
// once — so every type mismatch surfaces HERE, as a *ValidationError, not
// later in a handler. Unknown keys are ignored (they are not fields of T;
// rejecting request shape is an API-layer choice, not this package's).
func Bind[T any](body []byte, opts ...BindOption) (*Patch[T], error)

// BindReader is Bind over an io.Reader (sugar for http.Request.Body).
func BindReader[T any](r io.Reader, opts ...BindOption) (*Patch[T], error)

type BindOption func(*bindConfig)

// WithClearMode selects the clear policy (default ClearOnNull, §2.3).
func WithClearMode(m ClearMode) BindOption
```

Representative bind body (the only place the wire is parsed):

```go
raw := map[string]json.RawMessage{}
if err := json.Unmarshal(body, &raw); err != nil {
	return nil, &ValidationError{Fields: []FieldError{{Field: ".", Reason: err.Error()}}}
}
pl := planFor[T]()                       // §2.4, cached
p := &Patch[T]{mode: cfg.mode, fields: map[string]json.RawMessage{}, clear: map[string]struct{}{}}
for k, v := range raw {
	name, ok := pl.canonical(k)          // exact, then case-insensitive (json semantics)
	if !ok {
		if k == ClearKey && cfg.mode == ExplicitClear { /* pop + validate, below */ }
		continue                          // unknown key: ignored
	}
	if isNull(v) {
		switch cfg.mode {
		case ClearOnNull:
			p.clear[name] = struct{}{}    // null ⇒ clear
		case ExplicitClear:
			// LM semantics: null ⇒ absent (encoding/json would no-op anyway)
		}
		continue
	}
	p.fields[name] = v
}
// ExplicitClear: entries from $clear land in p.clear after shape validation
// and the value-AND-clear ambiguity check (both *ValidationError).
// Finally: one typed decode primes the cache and pins type errors to bind.
if _, err := p.Data(); err != nil {
	return nil, err
}
```

Binding through struct embedding also works: `Patch[T]` implements
`json.Unmarshaler` (same path, default mode), so a `Patch[Inner]` field
inside another bound type — or inside another `Patch[Outer]`'s `T` — parses
itself (§2.7).

### 2.3 Clear policy: `ClearMode` (`partial/clear.go`, new)

```go
// ClearMode declares what "clear this field" looks like on the wire.
type ClearMode uint8

const (
	// ClearOnNull (default): a JSON null value means "clear the field".
	// The ecosystem-natural reading of PATCH (RFC 7386 merge-patch uses it),
	// and the fix for LM's footgun where a client null silently no-opped.
	ClearOnNull ClearMode = iota

	// ExplicitClear (LM-compat): null means absent (ignored); clears ride
	// the reserved ClearKey ("$clear") array of field names. For wire
	// contracts migrated from LM, where clients already send $clear.
	ExplicitClear
)

// ClearKey is the reserved key naming the clear array in ExplicitClear
// mode. The `$` prefix is collision-free by construction: no exported Go
// field marshals to a `$`-prefixed name.
const ClearKey = "$clear"
```

Either mode ends in the same internal state (`fields` + `clear`), so
everything downstream — mutators, `Rules()`, `Data()` — is mode-agnostic.
Bind-time validation in both modes rejects the one ambiguous shape: a field
carrying a non-null value *and* marked cleared (`ValidationError`, field
named). Everything else — clearing a field the entity can't clear, unknown
names in `$clear` — deliberately flows through to no-op downstream at the
DAO's clearability authority (dao ADR-0010 §2.2), exactly LM's forgiving
wire posture.

### 2.4 The name plan: one cached reflect walk per T (`partial/plan.go`, new)

```go
// plan maps canonical effective JSON names to the fields of T.
type plan struct {
	byName map[string]planField // canonical name → field
	lower  map[string]string    // lower(name) → canonical (case-insensitive fallback)
	order  []string             // stable enumeration order
}

type planField struct {
	name  string      // effective JSON name: tag name, else Go field name
	index []int       // reflect index path (for embeds)
	typ   reflect.Type
}

var plans sync.Map // reflect.Type → *plan

func planFor[T any]() *plan
```

Resolution rules (mirroring `encoding/json`):

- `json:"name"` tag wins; `json:"-"` excludes; empty tag name falls back to
  the Go field name. Tag options (`omitempty`, `string`) are irrelevant to
  naming and ignored.
- Unexported fields are excluded.
- **Anonymous value embeds** recurse with JSON's flattening and conflict
  rules (shallowest wins; same-depth conflicts are dropped, as json does).
- **Anonymous pointer embeds panic at plan build** with a message naming
  the type and field — the loud fix for LM's silent cliff. A model bound as
  a partial payload must use value embeds; the panic fires on first use in
  dev, not as silently missing presence in prod.
- Wire-key lookup: exact match on canonical name first, then
  case-insensitive via `lower` — the same fallback `encoding/json` applies,
  so presence tracking and the typed decode can never disagree about which
  field a key fed.

### 2.5 Typed access (`partial/patch.go`)

```go
// Data returns the typed T decoded from the patch's value-bearing fields —
// cached after the first call (Bind primes it). Cleared and absent fields
// are T's zero values; presence is the patch's job, not T's.
func (p *Patch[T]) Data() (T, error)

// Get decodes one Present field into V without touching the rest of the
// patch. It is a package-level function because Go methods cannot introduce
// type parameters. State reports the field's disposition; V is only
// meaningful when it is Present.
func Get[V any, T any](p *Patch[T], field string) (v V, s State, err error)
```

`Data` decodes from the raw slots via one `json.Marshal`-free path: the
fields map is re-assembled into a single object decode (`json.Unmarshal`
over a synthesized `{...}` built from the retained raw slices — no `any`
round-trip, values pass through as raw bytes). `Get` decodes a single raw
slot. Both funnel through one internal `decode(raw, into)` so the future
json/v2 swap is local (§1.4).

### 2.6 Server-side mutators (`partial/patch.go`)

```go
// Set stages a server-owned value for field, replacing any bound value and
// dropping any pending clear — the last server mutation wins over a clear
// (a clear entry must not beat an injected value with a NULL write; LM
// payload.go:81-94). The value is marshaled once at Set time.
func (p *Patch[T]) Set(field string, v any) *Patch[T]

// Clear marks field cleared, dropping any value (a field is cleared or
// carries a value, never both).
func (p *Patch[T]) Clear(field string) *Patch[T]

// Remove strips field from BOTH channels — a server-controlled strip
// silently discards a client value and its clear intent alike.
func (p *Patch[T]) Remove(field string) *Patch[T]

// Only keeps the named fields, intersecting BOTH channels (allowlist).
func (p *Patch[T]) Only(fields ...string) *Patch[T]
```

Divergence from LM, deliberate: **mutators are a developer surface, so
unknown field names are loud.** A name that doesn't resolve through the
plan records a sticky first error (`fmt.Errorf("%w: %q", ErrUnknownField,
field)`) surfaced by `Err()`, `Data()`, `Rules()`, and `ApplyRules` — the
same fluent-but-loud posture as dao's `Set` (dao ADR-0010 §2.4 draws the
identical trust boundary from the other side: *its* lenient surface is the
wire-derived rules map, *its* loud surface is developer keys). LM's silent
as-is recording let a typo'd `Set("ArtistTitle", …)` vanish. All mutators
invalidate the `Data` cache.

### 2.7 Nesting contract

- **Whole-object replace by default.** A Present struct/slice/map field's
  raw value decodes into the field as-is; there is no recursive merge.
- **`Patch` composition is supported.** A field of type `Patch[Inner]`
  (or `*Patch[Inner]`) inside `T` receives its raw object during the typed
  decode and parses itself via `UnmarshalJSON` — nested partial semantics
  without deep-merge. The outer patch's `State("inner")` says whether the
  nested object appeared; the inner patch carries its own three states.
  Composition uses the default `ClearOnNull` mode (Go gives `UnmarshalJSON`
  no options channel); an LM-compat nested contract binds the inner type
  explicitly instead.
- **Deep-merge is out of scope v1** (revisit only with a concrete consumer
  and RFC 7386 on the table).

### 2.8 Errors (`partial/errors.go`, new)

```go
// ValidationError reports bind-time problems a client can fix: malformed
// JSON, a type mismatch on a field, an invalid $clear shape, or the
// value-AND-clear ambiguity. errors.As-able; no app error imports.
type ValidationError struct {
	Fields []FieldError
}

type FieldError struct {
	Field  string // canonical field name, "." for document-level problems
	Reason string
}

func (e *ValidationError) Error() string

// ErrUnknownField is the sticky mutator error for a name that is not a
// field of T. errors.Is-able.
var ErrUnknownField = errors.New("partial: unknown field")
```

Type mismatches are extracted from the bind-time decode
(`*json.UnmarshalTypeError` → `FieldError{Field: canonicalOf(err.Field),
Reason: "expected " + err.Type.String()}`), so the handler renders one 400
with per-field detail and nothing type-shaped survives past `Bind` (G4).

### 2.9 The DAO seam (`partial/rules.go`, new)

```go
// RuleKind mirrors the Write/Skip/Clear disposition of dao ADR-0010 without
// importing dao — the projection is data, the adapter is the coupling.
type RuleKind uint8

const (
	RuleSkip  RuleKind = iota
	RuleWrite          // Value carries the decoded Go value
	RuleClear
)

type Rule struct {
	Kind  RuleKind
	Value any
}

// Rules projects the patch: one entry per Present field (RuleWrite with the
// typed value from the one cached decode) and per Cleared field (RuleClear).
// Absent fields have no entry. Keys are canonical effective JSON names.
func (p *Patch[T]) Rules() (map[string]Rule, error)
```

And the thin adapter — the only file that imports `golib/dao`
(`partial/dao.go`, new):

```go
// ApplyRules stages the patch onto a DAO via dao ADR-0010's SetRules: field
// names cast to the DAO's field enum, kinds mapped 1:1. The error is the
// patch's (bind/mutator state) — check it before executing the verb. By the
// ADR-0010 contract, dao's SetRules is lenient on keys that don't resolve
// to writable fields and its Field declarations own clearability, so a
// patch may carry more than the entity writes without ceremony.
func ApplyRules[R any, C ~string, ID any, T any](
	d dao.DAO[R, C, ID], p *Patch[T], opts ...ApplyOption,
) (dao.DAO[R, C, ID], error) {
	rules, err := p.Rules()
	if err != nil {
		return d, err
	}
	m := make(map[C]dao.Rule, len(rules))
	for name, r := range rules {
		if cfg.rename != nil {
			name = cfg.rename(name) // escape hatch for name divergence
		}
		switch r.Kind {
		case RuleWrite:
			m[C(name)] = dao.Write(r.Value)
		case RuleClear:
			m[C(name)] = dao.Clear()
		}
	}
	return d.SetRules(m), nil
}

// WithRename installs a field-name translation for entities whose dao field
// enums diverge from their wire names. The default is the identity — the
// convention (dao ADR-0010 §2.6) is that an entity's field-enum values ARE
// its wire names.
func WithRename(fn func(string) string) ApplyOption
```

Name alignment stays a **declaration convention**: golib entities own both
the `json` tags and the dao field enums, so they agree by construction and
the adapter is a type cast. `WithRename` is the loud escape hatch, not the
default path. This is the end-to-end collapse of LM's cost center: PATCH
body → `Bind` → mutate → `ApplyRules(...).Update()`, zero per-entity code.

### 2.10 Marshal round-trip

`Patch[T]` implements `json.Marshaler`: Present fields emit their raw
slices; Cleared fields emit per mode (`null` under `ClearOnNull`, a
`$clear` array under `ExplicitClear`) — so a proxied or logged patch
round-trips without losing clear intent (LM's `Bytes` re-injection lesson,
`payload.go:330-346`, without the byte-cache coherence hazard: marshal is
derived fresh from the two channels every call).

### 2.11 North-star usage

```go
// ---- model: json tags and dao field enums agree by convention --------------
type Release struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	UPC   string  `json:"upc"`
	Date  string  `json:"release_date"`
}

// ---- handler: bind → validate → strip → apply → done ------------------------
func patchRelease(w http.ResponseWriter, r *http.Request) {
	p, err := partial.BindReader[Release](r.Body)
	if err != nil {
		var ve *partial.ValidationError
		if errors.As(err, &ve) { render400(w, ve); return }
		render500(w, err); return
	}
	p.Remove("id")                        // server-owned: strip both channels
	if p.Empty() { w.WriteHeader(http.StatusNoContent); return }

	d, err := partial.ApplyRules(releases.OnCtx(r.Context()).With(RelID, id), p)
	if err != nil { render400(w, err); return }
	if err := d.Update(); err != nil { renderDBErr(w, err); return }
}
// A body of {"title":"New","upc":null} writes title, clears upc (NULL),
// touches nothing else. {"release_date":null} clears to the dao Field's
// declared sentinel. {} no-ops. All of it: zero release-specific code.
```

---

## 3. Consequences

**Positive.** The wire half of the partial-update story lands with the
same declaration-over-code shape as the dao half: presence is parsed once,
validated once, keyed by one name space, and projected onto `SetRules`
mechanically. LM's five measured defects (multi-parse, name divergence,
uncached reflection, pointer-embed cliff, exposed mutable state) are each
structurally closed. The package is stdlib-only; only the one adapter file
touches `golib/dao`.

**Negative / costs.** `ClearOnNull` as default is a *semantic choice* a
migrating LM client must opt out of (`ExplicitClear`) — the compat mode
exists precisely so the default can be the honest one. Composition can't
carry a non-default mode through `UnmarshalJSON` (documented; Go gives no
options channel there). The plan cache panics on pointer embeds — loud by
design, but it is a runtime (first-use) failure, not compile-time; the
acceptance tests pin it. `Patch` methods can't be generic, so single-field
typed access is the package-level `Get[V]` — mildly unidiomatic, honestly
documented.

**Migration.** New package; nothing existing changes. LM-shaped consumers
adopt with `WithClearMode(partial.ExplicitClear)` and keep their wire
contract byte-compatible ($clear array, null-means-absent).

---

## 4. Alternatives considered

- **Map-backed payload (LM's shape: `map[string]any` + typed decode on
  demand).** Proven, but it is exactly the multi-parse/`any`-round-trip
  design the audit costed out; presence keyed by map membership invites the
  Go-name/wire-name split. Rejected for raw slots + one plan.
- **Presence via pointer fields (`*string` everywhere) or
  `Optional[T]`/`Null[T]` wrapper types on the model.** Infects every model
  definition and every consumer of the model with wire concerns; can't
  distinguish null from absent without a three-state wrapper on every
  field; LM's `nilable.Value` was on the drop list for this reason.
  Rejected: presence lives in the carrier, models stay plain.
- **`null` always means clear, no compat mode.** Cleaner, but strands LM
  wire contracts (their clients send `$clear` and use `null` as a no-op) —
  a migration cliff for the package's most likely early adopters. Rejected
  for the two-mode policy with the honest default.
- **`$clear` always, no null-clear mode.** Keeps LM's footgun as the only
  semantics: the ecosystem-natural `{"field":null}` silently no-ops — the
  exact surprise the plan calls out. Rejected as default; kept as the
  compat mode.
- **Deep-merge nested objects (RFC 7386 semantics).** Real semantics, real
  demand someday — but it multiplies the state model (per-path presence),
  interacts badly with slices, and no golib consumer needs it yet.
  Deferred; `Patch` composition covers the nested-partial case v1 needs.
- **Emitting `dao.Rule` directly from `Rules()`** (partial depends on dao
  everywhere). One less type, but the projection is useful without dao
  (logging, HTTP fan-out, tests) and the plan's boundary is "rules
  projection, not coupling" — one adapter file keeps the dependency edge
  explicit and severable. Rejected.
- **Validating unknown wire keys at bind.** Strict-shape APIs are a
  legitimate policy, but it is an API-layer policy, not payload semantics —
  and LM's ignore-unknown posture is load-bearing for shared payload shapes.
  Rejected as built-in; a consumer wanting it can diff `Fields()` against
  the body's keys… or we add a `WithDenyUnknown` BindOption when someone
  actually asks.
- **`encoding/json/v2` now.** `GOEXPERIMENT`-gated in Go 1.25; a library
  cannot impose that on consumers. Deferred behind the two decode funnels
  (§1.4, §2.5).

---

## 5. Acceptance criteria

1. **Three states bind correctly (ClearOnNull).** For
   `{"title":"x","upc":null}` against a 4-field model: `State("title") ==
   Present`, `State("upc") == Cleared`, the other fields `Absent`;
   `Data().Title == "x"`; `Empty() == false`; `{}` binds `Empty() == true`.
2. **ExplicitClear compat.** The same body in `ExplicitClear` mode reports
   `upc` **Absent** (null means absent); `{"title":"x","$clear":["upc"]}`
   reports `upc` Cleared; a non-array `$clear` and a value-AND-clear
   conflict each return a `*ValidationError` naming the field.
3. **Bind-time typed validation.** `{"title":123}` returns
   `*ValidationError` with `Field == "title"` from `Bind` — no error
   deferred to `Data()`.
4. **One name space.** A model with `json:"artist_title"` tracks presence
   under `artist_title` (not the Go field name); a wire key differing only
   in case still binds to the same canonical name (matching
   `encoding/json`'s case-insensitive fallback).
5. **Plan hardening.** A value embed's fields participate (flattened, JSON
   conflict rules); an anonymous *pointer* embed panics at first use with
   the type and field named; `planFor[T]` reflects once per T (the walk is
   observably cached).
6. **Mutator semantics.** `Set` after bind replaces the value AND drops a
   pending clear (last-mutation-wins-over-clear); `Remove` strips both
   channels; `Only` intersects both; each invalidates `Data()`'s cache; a
   mutator on an unknown field surfaces `ErrUnknownField` (errors.Is) from
   `Err()`/`Rules()` while valid mutations before it still applied.
7. **Rules projection.** For bind `{"title":"x","upc":null}` + `Set("grid",
   "g")`: `Rules()` = `{title: Write("x"), grid: Write("g"), upc: Clear}`,
   nothing else; `ApplyRules` onto a dao fake stages exactly those via
   `SetRules` (UPDATE SQL asserts title/grid written, upc NULL) — proving
   the dao ADR-0010 §2.6 end-to-end contract with zero per-entity code.
8. **Composition & round-trip.** A `T` carrying a `Patch[Inner]` field
   binds: outer `State("inner") == Present`, inner patch reports its own
   three states from the nested object. `json.Marshal` of a patch
   round-trips through `Bind` preserving all three states in both modes.

## 6. File plan

| File | Change |
|---|---|
| `partial/patch.go` | new — `Patch[T]`, `State`, read surface, mutators, `Data`, `Get`, marshal |
| `partial/bind.go` | new — `Bind`, `BindReader`, `BindOption`, `UnmarshalJSON`, bind validation |
| `partial/clear.go` | new — `ClearMode`, `ClearKey`, clear-channel resolution |
| `partial/plan.go` | new — per-`T` cached name plan, embed rules, pointer-embed panic |
| `partial/errors.go` | new — `ValidationError`, `FieldError`, `ErrUnknownField` |
| `partial/rules.go` | new — `RuleKind`, `Rule`, `Patch.Rules()` |
| `partial/dao.go` | new — `ApplyRules`, `ApplyOption`, `WithRename` (the only dao import) |
| `partial/doc.go` | new — package doc: three-state model, modes, the dao seam |
| `partial/*_test.go` | new — acceptance criteria 1–8 (dao fake for #7) |
| `partial/README.md` | new — usage, the north-star handler, LM-compat migration note |
