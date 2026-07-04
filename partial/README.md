# `golib/partial`

Presence-aware partial (PATCH) payloads for a model type `T`, with a
zero-per-entity seam onto `golib/dao`'s partial-update rules (dao ADR-0010).

An HTTP PATCH body is a *three-state* document: each field either **carries a
value**, is **omitted**, or is asked to be **cleared**. `encoding/json`
collapses the last two — decoding into a struct leaves both an omitted field
and an explicit `null` at the zero value. `partial.Patch[T]` tracks that state
out of band from the typed struct, parses the body once, validates at bind
time, and projects a Write/Skip/Clear disposition the DAO consumes directly.

Design: [`docs/partial/adr-0001-generic-partial-payload-package.md`](../docs/partial/adr-0001-generic-partial-payload-package.md).

## Usage

```go
type Release struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	UPC   string `json:"upc"`
	Date  string `json:"release_date"`
}

func patchRelease(w http.ResponseWriter, r *http.Request) {
	p, err := partial.BindReader[Release](r.Body)
	if err != nil {
		var ve *partial.ValidationError
		if errors.As(err, &ve) { render400(w, ve); return }
		render500(w, err); return
	}
	p.Remove("id")                     // server-owned: strip both channels
	if p.Empty() { w.WriteHeader(http.StatusNoContent); return }

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

All of it with zero release-specific code: field→column comes from the dao
`Schema`, clearability from the `Field` declaration, name alignment from the
convention that an entity's field-enum values are its wire (JSON) names.

## Three states

| Wire (`ClearOnNull`, default) | `Patch` state | On write |
|---|---|---|
| key present, non-null value | `Present` | write the value |
| key absent | `Absent` | skip |
| key present, `null` value | `Cleared` | write the cleared state |

## Clear modes

- **`ClearOnNull`** (default) — a JSON `null` clears. The ecosystem-natural
  PATCH reading (RFC 7386), and the fix for the footgun where a client `null`
  silently no-ops.
- **`ExplicitClear`** (LM-compat) — `null` means *absent*; clears ride a
  reserved `"$clear": ["field", ...]` array. Select it with
  `partial.Bind[T](body, partial.WithClearMode(partial.ExplicitClear))` to keep
  a wire contract byte-compatible with a Label-Manager-style client.

## Server-side mutation

`Set` / `Clear` / `Remove` / `Only` shape a bound patch. Last mutation wins
over a clear (an injected `Set` value beats a pending clear). Unlike wire keys
(ignored when unknown), a mutator naming a field that isn't part of `T` is a
developer bug and surfaces a sticky `ErrUnknownField` via `Err()`, `Data()`,
`Rules()`, and `ApplyRules`.

## Notes

- **Names.** Presence, mutation, rules, and the typed decode all key by the
  *canonical effective JSON name* (tag name, else Go field name) from a
  per-`T` cached plan. A model bound as a partial payload must use value
  embeds (anonymous pointer embeds panic at first use) and may not declare a
  field whose JSON name is `$clear`.
- **Stdlib only**, except `partial/dao.go` — the single file importing
  `golib/dao` for the `ApplyRules` adapter. `Rules()` itself is dao-free.
