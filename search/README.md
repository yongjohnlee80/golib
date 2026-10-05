# search — hybrid search over a store you own

`search` runs a search the way a local knowledge base needs it: words and meaning, fused. Lexical
retrieval and semantic retrieval each give their best candidates. Reciprocal rank fusion merges
them, links and tags boost them, and a cap keeps one document from filling the page. The engine
holds the logic; your application holds the data. You keep your own tables, migrations and SQL,
and implement a small port, one consistent snapshot per query, and the engine reads only through it.
A fix or an improvement here reaches every store at once.

| package | what it holds |
| --- | --- |
| `search` | the types, the `Chunkers` registry, the store port, the `Engine` and the `Searcher` interface |
| [`search/chunk`](chunk/README.md) | Markdown, plain-text and YAML chunkers, and a Markdown metadata reader |
| [`search/embed`](embed/README.md) | the embedding provider contract, Ollama and OpenAI-compatible clients, `Bisect` |
| [`search/rank`](rank/README.md) | the re-ranking stage: the `Ranker` contract, TEI and Cohere-style clients, a `Searcher` that re-ranks another's top hits |
| [`search/vector`](vector/README.md) | dot products, 1-bit codes and Hamming distance, a per-model code index, the two-stage scan |
| [`search/query`](query/README.md) | a query's words as literal terms, `name:value` filters, SQLite FTS5 rendering |
| [`search/searchtest`](searchtest/README.md) | a conformance suite for your store, and `MemStore`, a reference store in memory |

No package here imports a third-party module.

## Install

```sh
go get github.com/yongjohnlee80/golib
```

```go
import "github.com/yongjohnlee80/golib/search"
```

## Searching

```go
eng := search.NewEngine[int64, *myView](store, search.WithQueryEmbedder(embedQuery))
res, err := eng.Search(ctx, search.Query{
	Text:   "type:adr storage compaction",
	Filter: search.Filter{Tags: []string{"design"}, Paths: []string{"guides"}},
	Fields: schema, // which name:value words are filters
})
for _, h := range res.Hits {
	fmt.Println(h.Path, h.Breadcrumb, h.Relevance, h.Via)
}
// res.ModeUsed: lexical, semantic, hybrid or facet; res.Semantic: off, ready, partial, switching or error
```

`Query.Mode` asks for `auto` (hybrid when vectors can answer), `lexical` or `semantic`. A semantic
search that cannot run is degraded in `auto` and refused in `semantic`, and `Result.Semantic` always
says which it was:

| state | `auto` | `semantic` | `lexical` |
| --- | --- | --- | --- |
| off (no embedder, or a store without vectors) | lexical | `ErrNoProvider` | lexical |
| partial (some documents not yet embedded) | hybrid over the ready ones | semantic over the ready ones | lexical |
| switching (a new model filling) | lexical | `ErrSwitching` | lexical |
| error (the query could not be embedded) | lexical | `ErrEmbedFailed` | lexical |
| ready | hybrid | semantic | lexical |

A query with no words embeds nothing, so the table does not apply to it: in every mode, with an
embedder or without, its filters list their documents (`ModeFacet`), and without filters it
answers no hits.

## Implementing a store

A store is `View(ctx, fn)`: it opens one consistent snapshot, such as a read transaction, and hands
fn a view of it. Every view has `Lexical`. A view type may add capabilities, each an interface
carrying the operations it gates:

| interface | adds | without it |
| --- | --- | --- |
| `View[D]` | `Lexical` | (required) |
| `Semantic[D]` | `Semantic`, `SemanticState` | the store searches by words only |
| `Signaler[D]` | `Signals`: in-links and tags | nothing is boosted |
| `Lister[D]` | `List`: documents by filters alone | a filters-only query is `ErrNoLister` |

`D` is your document key, which the engine only compares for equality. It can be an int64, a UUID,
or a tenant and id pair. `NewEngine` decides what a store can do once, from the view type's static
method set. Build the engine over your concrete view type, or over an interface that names the
capabilities you mean. Over `View[D]` alone, every store is lexical-only, whatever its values could
do.

A sketch over SQLite:

```go
type store struct{ db *sql.DB }
type view struct{ tx *sql.Tx }

func (s *store) View(ctx context.Context, fn func(*view) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}) // one snapshot (WAL)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(&view{tx}) // fn's error as it is, so the engine can recognize its own
}

func (v *view) Lexical(ctx context.Context, terms []search.Term, f search.Filter, n int) ([]search.Candidate[int64], error) {
	match := query.FTS5(terms) // literal words: no FTS5 syntax gets through
	// SELECT … WHERE chunk_fts MATCH ? AND <f's tags, paths and facets> ORDER BY rank, path LIMIT ?
}

func (v *view) Semantic(ctx context.Context, model string, vec []float32, f search.Filter, n int) ([]search.Candidate[int64], error) {
	// read the active model and the commit watermark in this transaction
	if active != model {
		return nil, search.ErrModelChanged
	}
	codes := published.Load() // a *vector.Index you keep up to date after each commit
	var seq iter.Seq[vector.Code[int64]]
	if codes.Usable(model, watermark) {
		seq = codes.Codes()
	} else {
		seq = v.storedCodes(model) // the complete scan, in this same transaction
	}
	order := vector.Nearest(seq, vector.SignBits(vec), cmp.Compare[int64])
	best, err := vector.TwoStage(order, vec, 200, n, v.validWithVectors(f), lessByPathThenOrd)
	// candidates from best
}
```

A pgvector store answers `Semantic` with `ORDER BY embedding <=> $1 LIMIT n` instead. Either way, the
contract is that of the doc comments on `Store`, `View`, `Semantic`, `Signaler` and `Lister`:

- filters apply before each retriever's rank and limit;
- equal ranks are ordered by path: lexically, then the backend's own stable order; semantically, then
  ordinal;
- every candidate's text, path and generation come from the view's snapshot;
- a semantic view answers only from alive chunks of documents ready under the active model.

Then prove it with the suite:

```go
func TestMyStore(t *testing.T) {
	searchtest.Run(t, func(t testing.TB, docs []searchtest.Doc) searchtest.Fixture[int64, *view] {
		// index docs through your own write path, storing searchtest.Embed(body) as each vector
	})
}
```

## Chunkers

`Chunkers` maps file extensions to `Chunker`s. It is a value your application builds; there is no
global registry. A `Chunker` reports a `Version`. An index that records it knows to cut its
documents again when it changes:

```go
var r search.Chunkers
_ = r.Register(".md", chunk.MarkdownChunker{})
_ = r.Register(".adoc", myAsciiDoc{}) // any type with Version and Chunk
c, ok := r.For("notes/guide.adoc")
```

`Chunk.Embed` is the text to embed when it differs from the breadcrumb and body. A code chunker can
index a function's source and embed its signature and documentation. `Chunk.TextHash` is the
embedding's identity: two chunks with the same text share a vector.

## Swapping the engine

Hold a `search.Searcher`, not an `*Engine`, and any engine that answers `Search(ctx, Query)
(Result, error)` can take its place without changing the code that searches.

## Errors

| error | when |
| --- | --- |
| `ErrUnknownMode` | a mode that is not `auto`, `lexical` or `semantic` |
| `ErrNoProvider` | `semantic` asked of an engine without an embedder, or over a store without vectors, for a query with words |
| `ErrNoLister` | a query of filters alone, over a store that cannot list |
| `ErrSwitching` | `semantic` while a new model fills (and what a query embedder answers then) |
| `ErrEmbedFailed` | `semantic` when the query could not be embedded |
| `ErrModelChanged` | a semantic view whose active model is not the query's; the engine embeds again, up to three times |
| `ErrChunkerTaken` | registering a second chunker for an extension |
| `query.ErrUnknownFacet`, `query.ErrFacetValue` | a filter on an undeclared field, or a value not of its type |

## License

See [LICENSE](../LICENSE).
