# partial

Presence-aware partial (PATCH) payloads for a model type `T`, with a
zero-per-entity seam onto `golib/dao`'s partial-update rules (dao ADR-0010).
Stdlib-only, except the single `dao.go` file that provides the dao adapter.

An HTTP PATCH body is a *three-state* document: each field either **carries a
value**, is **omitted**, or is asked to be **cleared**. `encoding/json`
collapses the last two — decoding into a struct leaves both an omitted field
and an explicit `null` at the zero value. `partial.Patch[T]` tracks that state
out of band from the typed struct, parses the body once, validates at bind
time, and projects a Write/Skip/Clear disposition the DAO consumes directly.

Design: [`docs/partial/adr-0001-generic-partial-payload-package.md`](../docs/partial/adr-0001-generic-partial-payload-package.md).

```bash
go get github.com/yongjohnlee80/golib/partial
```

## The end-to-end flow

```go
type Release struct {
    ID    string `json:"id"`
    Title string `json:"title"`
    UPC   string `json:"upc"`
    Date  string `json:"release_date"`
}

func patchRelease(w http.ResponseWriter, r *http.Request) {
    // 1. Bind: one parse, validated at bind time.
    p, err := partial.BindReader[Release](r.Body)
    if err != nil {
        var ve *partial.ValidationError
        if errors.As(err, &ve) { render400(w, ve); return } // bad JSON, type mismatch, $clear shape
        render500(w, err); return
    }

    // 2. Shape it server-side (chainable).
    p.Remove("id")                     // server-owned: strip from both channels
    if p.Empty() { w.WriteHeader(http.StatusNoContent); return }

    // 3. Project onto the dao and run the terminal verb.
    d, err := partial.ApplyRules(releases.OnCtx(r.Context()).With(RelID, id), p)
    if err != nil { render400(w, err); return }
    if err := d.Update(); err != nil { renderDBErr(w, err); return }
}
```

- `{"title":"New","upc":null}` — writes `title`, clears `upc` to SQL NULL,
  touches nothing else.
- `{"release_date":null}` — clears to the dao `Field`'s declared `ClearValue`
  sentinel (a NOT NULL column).
- `{}` — no-op.

Zero release-specific code: field→column comes from the dao `Schema`,
clearability from the `Field` declaration, name alignment from the convention
that an entity's field-enum values are its wire (JSON) names.

## Three states

`Patch[T]` distinguishes the three dispositions out of band:

| Wire (`ClearOnNull`, default) | `Patch` state | On write |
|---|---|---|
| key present, non-null value | `Present` | write the value |
| key absent | `Absent` | skip |
| key present, `null` value | `Cleared` | write the cleared state |

Query them directly:

```go
p.State("upc")        // partial.Present | partial.Cleared | partial.Absent
p.Present("title")    // State == Present
p.Cleared("upc")      // State == Cleared
p.Contains("a", "b")  // every named field is Present
p.Fields()            // sorted canonical names carried (Present + Cleared)
p.Empty()             // no values AND no clears (a clear-only patch is NOT empty)
```

Typed access reads value-bearing fields:

```go
title, state, err := partial.Get[string](p, "title") // decode one Present field
full, err := p.Data()                                 // the whole T from value-bearing fields (cached)
```

> `Get` is a package-level function, not a method — a Go method cannot introduce
> the result type parameter.

## Clear modes

- **`ClearOnNull`** (default) — a JSON `null` clears. The ecosystem-natural
  PATCH reading (RFC 7386), and the fix for the footgun where a client `null`
  silently no-ops.
- **`ExplicitClear`** (LM-compat) — `null` means *absent*; clears ride a
  reserved `"$clear": ["field", ...]` array. Select it to keep a wire contract
  byte-compatible with a Label-Manager-style client:

```go
p, err := partial.Bind[Release](body, partial.WithClearMode(partial.ExplicitClear))
```

Both modes converge on the same internal state, so everything downstream
(mutators, `Rules()`, `Data()`) is mode-agnostic. A field that is both
value-bearing and listed in `$clear` is rejected at bind as a
`*ValidationError`.

## Server-side mutation

`Set` / `Clear` / `Remove` / `Only` shape a bound patch; all return `*Patch[T]`
for chaining. Last mutation wins over a clear (an injected `Set` value beats a
pending clear).

```go
p.Set("updated_by", uid).  // inject a server value
  Remove("id").            // strip a server-owned field (both channels)
  Only("title", "upc")     // allowlist — intersect down to these
```

Leniency differs by intent: unknown **wire** keys are ignored, and `Remove`/
`Only` treat an unknown field as a no-op. But `Set`/`Clear` naming a field that
isn't part of `T` is a developer bug and records a sticky `ErrUnknownField`
surfaced by `Err()`, `Data()`, `Rules()`, and `ApplyRules`.

## Projecting: `Rules()` and the dao seam

`Rules()` projects the patch to a dao-free disposition map — useful for logging,
tests, or fan-out without importing dao:

```go
rules, err := p.Rules() // map[string]partial.Rule{Kind: RuleWrite|RuleClear, Value: ...}
```

`ApplyRules` is the dao adapter (the only place `partial` imports `dao`). It
maps the projection to `dao.SetRules` — `RuleWrite → dao.Write(v)`,
`RuleClear → dao.Clear()`, absent fields produce no entry — and returns the
staged DAO:

```go
d, err := partial.ApplyRules(schema.OnCtx(ctx).With(ID, id), p)
```

The seam is a contract, not a coupling: `dao` never imports `partial`.
Per-column clearability lives on the dao `Field` (`Clearable`, `ClearValue`),
and `dao.SetRules` silently skips keys that don't resolve to a writable field —
the wire-facing counterpart to dao's loud `Set`/`SetMap`. When the entity's
field enum diverges from wire names, translate with `WithRename`:

```go
d, err := partial.ApplyRules(dao, p, partial.WithRename(strings.ToLower))
```

## Composition

`Patch[T]` implements both `json.Marshaler` and `json.Unmarshaler`, so a
`Patch[Inner]` field inside a larger type parses itself and a patch round-trips
through JSON preserving all three states. Nested binding always uses the default
`ClearOnNull` mode (Go gives `UnmarshalJSON` no options channel) — bind the
inner type explicitly if you need `ExplicitClear`.

## Gotchas

- **Panics are first-use (runtime), not compile-time.** Building the per-`T`
  field plan panics on a non-struct `T`, an anonymous **pointer** embed, or a
  model field whose JSON name is `$clear`. Use value embeds.
- **Empty counts both channels** — gate no-ops on `p.Empty()`, not
  `len(p.Fields()) == 0`.
- **`ValidationError.Fields`** carries per-field `FieldError{Field, Reason}`
  (`Field == "."` for document-level errors) — render it for a 400.
- Unknown wire keys are always ignored; strict-shape rejection is an API-layer
  choice, not built in.

## File layout

| File | Contents |
|---|---|
| `patch.go` | `Patch[T]`, `State`, read surface, mutators, `Get`, `Data`, `MarshalJSON` |
| `bind.go` | `Bind`, `BindReader`, `WithClearMode`, `UnmarshalJSON` |
| `clear.go` | `ClearMode`, `ClearKey` |
| `rules.go` | `Rule`, `RuleKind`, `Rules()` |
| `dao.go` | `ApplyRules`, `WithRename` (the only dao import) |
| `errors.go` | `ValidationError`, `FieldError`, `ErrUnknownField` |

## License

See [LICENSE](../LICENSE).