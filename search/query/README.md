# search/query — a search query's words and filters

`Terms` splits a query into literal words, so no full-text dialect's syntax gets through. A trailing
`*` on the last word makes it a prefix. `FTS5` renders terms as an SQLite FTS5 match. `Facets` takes
a query's `name:value` words for declared fields out as filters, and leaves the others as words.

## Install

```go
import "github.com/yongjohnlee80/golib/search/query"
```

## Example

```go
terms, words := query.Terms(`storage OR "x" stor*`)
// terms: storage, OR, "x", stor (prefix); words (for tag boosts): storage, or, "x", stor
match := query.FTS5(terms) // "storage" "OR" """x""" "stor"*
ts, err := query.TSQuery(terms) // 'storage' & 'OR' & '"x"' & 'stor':*   (PostgreSQL; no terms: ErrNoTerms)

rest, facets, err := query.Facets("type:adr re:x storage", nil, schema)
// with type declared: rest "re:x storage", facets {type: [adr]}
```

`Fields` declares the fields: `Declared(field)` and `FacetValue(field, text)`, which reads a value as
its field's type. A nil `Fields` declares none. A pointer type whose nil value means "no schema"
must answer `false` from `Declared` on a nil receiver.

| error | when |
| --- | --- |
| `ErrUnknownFacet` | a given filter on a field that is not declared |
| `ErrFacetValue` | a value `FacetValue` refuses, or a given field with no values |

## License

See [LICENSE](../../LICENSE).
