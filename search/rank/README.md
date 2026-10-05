# search/rank — a re-ranking stage, and its rankers over plain HTTP

Search's second stage. Recall (lexical and semantic, fused) finds candidates; a **ranker**, a
cross-encoder that reads the query and each candidate together, scores them, and the best come
first. A rare term that a vector dilutes (a part number, an error code) counts again here.

A `Ranker` scores texts against a query: `Rank(ctx, query, texts)` answers one score for each
text, in the texts' order, or an error. `Model` names it. It only sorts: the caller prepares the
texts it reads, in any format. `NewTEI` speaks Hugging Face Text Embeddings Inference's `/rerank`,
and refuses a server whose model is not a re-ranker. `NewRerankAPI` speaks the Cohere-style
`/rerank`, which Cohere, Jina, Voyage, vLLM, Infinity and llama.cpp's server serve. Both use
`net/http`, with no SDK, and both meter their calls as `search/embed`'s providers do.

## Install

```go
import "github.com/yongjohnlee80/golib/search/rank"
```

## Example

```go
r, err := rank.NewTEI(ctx, "http://tei:8080", "", nil)

var h rank.Holder
h.Set(r, 40) // rank the top 40 candidates; set again to swap rankers, nil for none

texts := func(ctx context.Context, hits []search.Hit) ([]string, error) {
	return myChunkTexts(ctx, hits) // or records rendered as you like: the ranker reads whatever this answers
}
s := rank.NewSearcher(engine, texts, &h)
res, err := s.Search(ctx, search.Query{Text: "where do we retry a failed embed", Limit: 10})
// res.Rank.State is ready (the ranker's order), off (nothing ranked) or error (recall order)
```

## The stage

- **Limits.** The inner Searcher is asked for the larger of the window and the query's limit. The
  window's top hits are ranked, any beyond it follow in recall order, and the answer is cut to the
  query's limit (0: `WithDefaultLimit`, 20; at most `WithMaxLimit`, 200), ranked or not.
- **Scores stay apart from recall.** A ranked hit gains `RankScore`, comparable within one result,
  and `"rank"` appended to `Via`. `Relevance` keeps its recall meaning. Equal scores keep their
  recall order.
- **Each distinct text is sent once.** Hits whose texts are identical share its score; no hit is
  dropped.
- **Failure falls back to recall,** saying so: the ranker's or the text source's error, or an
  answer of the wrong length, leaves the hits in recall order with `Rank.State` `error` and a
  constant message. With `rank.Required()`, the search fails instead (`ErrUnavailable`, wrapping
  the cause), and so does a worded search with no ranker in use.
- **Pass-through.** A facet listing and a query with no words are answered in recall order, with
  `Rank.State` `off`.
- **The ranker is live.** A search reads its `Source` (a `Holder`, say) once, at its start, and
  finishes with that ranker; setting the Holder never waits for a search.

| error | when |
| --- | --- |
| `ErrUnreachable` | the ranker did not answer |
| `ErrUnauthorized` | the ranker refused the key (HTTP 401, 403) |
| `ErrNoModel` | the ranker has no such model (HTTP 404) |
| `ErrNotARanker` | a TEI server whose model is not a re-ranker |
| `ErrRateLimited` | the ranker's usage limit is reached (HTTP 429) |
| `ErrRefused` | any other refusal |
| `ErrBadAnswer` | an answer that is not one score for each text |
| `ErrStale` | a text source's hit that is no longer in the index |
| `ErrUnavailable` | a `Required` Searcher that could not rank |

A provider's own error text is never in an error: these say what happened.

## License

See [LICENSE](../../LICENSE).
