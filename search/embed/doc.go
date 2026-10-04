// Package embed is the embedding provider contract and its HTTP clients: Ollama's /api/embed, and
// any OpenAI-compatible /v1/embeddings endpoint, both plain net/http with no SDK. Bisect embeds a
// batch, narrowing a provider's rejection of the input down to the texts it refuses.
//
//	p, err := embed.NewOllama(ctx, embed.DefaultOllamaURL, "", "bge-m3", nil, embed.WithContext(2048))
//	vecs, rejected, err := embed.Bisect(ctx, p, texts, p.Model().Dims, 30*time.Second)
//
// A provider is optional to a search engine: with none, search is by words.
package embed
