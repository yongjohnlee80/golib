// Package rank implements the second-stage re-ranking pipeline in the search subsystem.
// It scores candidate texts against the search query using cross-encoders that evaluate
// each (query, text) pair jointly for precise semantic relevance.
//
// # Architecture and Pipeline Components
//
//   - [Ranker]: Interface for cross-encoder scoring backends (such as Text Embeddings Inference / TEI).
//   - [NewSearcher]: Decorator wrapping an underlying [search.Searcher]. It requests max(window, limit)
//     candidates from the inner search, scores up to window candidates with the ranker via [TextSource],
//     and leaves any remaining tail in original recall order.
//   - Fallback and Status: When no ranker is configured, [Searcher] passes results through with
//     RankOff. When ranking or text retrieval fails, it retains original recall order with RankError
//     (unless configured in Required mode, where non-empty, non-facet queries fail). Parent-context
//     cancellation always returns ctx.Err().
package rank
