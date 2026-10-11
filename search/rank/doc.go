// Package rank implements the second-stage re-ranking pipeline in the search subsystem.
// It scores candidate texts against the search query using cross-encoders that evaluate
// each (query, text) pair jointly for precise semantic relevance.
//
// # Architecture and Pipeline Components
//
//   - [Ranker]: Interface for cross-encoder scoring backends (such as Text Embeddings Inference / TEI).
//   - [NewSearcher]: Decorator wrapping an underlying [search.Searcher]. It intercepts the top-K
//     candidates returned by the primary retriever, fetches candidate texts via [TextSource],
//     and sorts them by cross-encoder relevance score.
//   - Graceful Degradation: If no ranker is configured or the upstream ranking service experiences
//     transient timeouts, [Searcher] falls back to the initial retrieval order (noting degradation
//     in [search.Result.Rank]) rather than failing the search, unless configured in Required mode.
package rank
