// Package search provides a hybrid information retrieval engine combining lexical
// (full-text) search, semantic vector retrieval, reciprocal rank fusion (RRF),
// graph-based link/tag boosting, and optional cross-encoder re-ranking.
//
// # Architectural Model
//
// The search subsystem decouples storage, chunking, embedding, and ranking into modular
// abstractions:
//
//   - [Engine]: The core coordinator parameterized over document key D and view V ([View]).
//     It queries the store snapshot, collects candidate hits from lexical and semantic
//     retrievers, applies Reciprocal Rank Fusion, applies domain boosts (in-links, tags),
//     and enforces per-document result caps.
//   - [Store]: The application-owned storage contract. The application manages tables, SQL,
//     and migrations, providing a consistent point-in-time [View] snapshot for each search query.
//   - Capability Segregation: Storage capabilities are decoupled via interface composition on [View]:
//   - [View]: Baseline lexical retrieval (full-text search).
//   - [Semantic]: Vector similarity search over embedded document chunks.
//   - [Signaler]: In-link counts and document tags for relevance boosting.
//   - [Lister]: Direct faceted and metadata-filtered document listing.
//   - [Searcher]: Common query execution interface implemented by [Engine] and extended
//     by decorator pipelines (such as [golib/search/rank.NewSearcher]).
//
// # Subpackages
//
//   - [golib/search/chunk]: Document chunking for Markdown, YAML, and plain text.
//   - [golib/search/embed]: Client integrations for local Ollama and OpenAI-compatible embedding APIs.
//   - [golib/search/query]: Query tokenization, facet extraction, and SQLite FTS5 query synthesis.
//   - [golib/search/rank]: Two-stage cross-encoder re-ranking with fallback resilience.
//   - [golib/search/vector]: Vector math, 1-bit sign quantization, and two-stage candidate scanning.
//   - [golib/search/searchtest]: Conformance test harness for verifying Store implementations.
package search
