// Package rank implements the second-stage re-ranking pipeline in the search subsystem.
// It scores candidate texts against the search query using cross-encoders that evaluate
// each (query, text) pair jointly for precise semantic relevance.
//
// # Architecture and Pipeline Components
//
//   - [Ranker]: Interface for cross-encoder scoring backends (such as Text Embeddings Inference / TEI).
//   - [NewSearcher]: Decorator wrapping an underlying [search.Searcher]. When a ranker is in use,
//     it requests max(window, limit) candidates from the inner search and scores up to window candidates
//     with the ranker via [TextSource]; any remaining tail follows in original recall order, though
//     in [Required] mode the window is raised to at least the limit so no unranked tail is returned.
//   - Fallback and Status: When no ranker is configured, [Searcher] passes results through with
//     RankOff (or returns [ErrUnavailable] with no hits under [Required] mode, unless the query has no
//     words or is a facet listing). When ranking or text retrieval fails, it retains original recall order
//     with RankError (or [ErrUnavailable] under Required). Parent-context cancellation returns ctx.Err()
//     when ranking fails while the context is done.
package rank
