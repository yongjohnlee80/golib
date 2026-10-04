# search/searchtest — prove a store answers as the engine expects

`Run` indexes a fixed corpus through your store's own write path and checks it against the
`search` port's contract:
- lexical matching and `n`;
- the filter semantics, applied before the limit: tags ANDed, paths ORed by directory, facet
  fields ANDed and their values ORed;
- tie order;
- one snapshot per `View`, with a change committed during a view;
- the semantic, signals and listing capabilities, when the view type has them.

## Install

```go
import "github.com/yongjohnlee80/golib/search/searchtest"
```

## Example

```go
func TestMyStore(t *testing.T) {
	searchtest.Run(t, func(t testing.TB, docs []searchtest.Doc) searchtest.Fixture[int64, *myView] {
		s := openMyStore(t)
		for _, d := range docs {
			s.write(d) // storing searchtest.Embed(body) as each chunk's vector
		}
		return searchtest.Fixture[int64, *myView]{Store: s, Model: s.model, Change: s.rewrite}
	})
}
```

The suite checks the port from outside, so it cannot produce your store's internal races. A store
with an in-memory index also needs its own cells for those (see `vector.Index`).

`MemStore` is a store held in memory that passes the suite: a reference implementation, and a store
for tests of code that searches.

## License

See [LICENSE](../../LICENSE).
