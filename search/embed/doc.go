// Package embed provides client interfaces and HTTP implementations for vector embedding
// services used in semantic search.
//
// # Supported Providers
//
//   - Ollama ([NewOllama]): Native integration with Ollama's local /api/embed API.
//   - OpenAI-Compatible ([NewOpenAI]): Standard /v1/embeddings endpoint integration for
//     cloud models using standard net/http with zero SDK dependencies.
//
// # Setting a Model Up
//
// [Pull] has an Ollama server pull a model with its progress; [Catalog] is the measured cards of
// the models known so far, and [Card.Fits] says whether one runs on a [Budget]'s GPU or CPU.
//
// # Resilient Batch Ingestion
//
// When upstream embedding providers reject large text batches (due to token length limits,
// invalid byte sequences, or context window overflow), [Bisect] divides the batch recursively,
// isolating problematic documents while embedding valid candidates cleanly.
//
// Embedding providers are optional to [golib/search.Engine]; without a configured embedder,
// the engine operates in lexical (keyword/FTS5) mode.
package embed
