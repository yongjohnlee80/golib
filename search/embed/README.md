# search/embed — embedding providers over plain HTTP

A `Provider` turns texts into vectors: `Name`, `Model` (whose `Fingerprint` names it exactly) and
`Embed`. `NewOllama` speaks Ollama's `/api/embed`, local or Ollama Cloud. `NewOpenAI` speaks any
OpenAI-compatible `/v1/embeddings`. `NewVertex` speaks Vertex AI's `:embedContent` for
`gemini-embedding-2`. All use `net/http`, with no SDK. Each probes the model when it is made, so a
missing model fails at setup rather than at the first document.

A search's query goes through `Query(ctx, p, q)`: a provider that formats a query apart from a
document (a `Querier`, as `Vertex` is) embeds it with `EmbedQuery`; any other embeds it as a
document.

## Vertex AI

`NewVertex` takes a `TokenFunc` rather than credentials, so golib takes no Google dependency: the
caller answers an OAuth access token, from `golang.org/x/oauth2/google`'s default credentials, say.
`refresh` is true after the API refused the last token (401); the call is then sent once more, and
a second refusal is `ErrUnauthorized`. Every call carries `X-Goog-User-Project`.

- **Where.** `Location` is `global`, `us` or `eu` (`VertexHost` gives each one's host);
  `Endpoint` overrides the host.
- **One text a call.** `:embedContent` takes one content, and several parts would make one joint
  vector, so `Embed` sends each text in its own call.
- **Google's prompt formats.** A document is sent as `title: none | text: {text}`, a query as
  `task: search result | query: {query}`. Their version, `VertexFormat`, is the model's `Digest`,
  so a change of format is a change of model.
- **Size.** `Dimensions` (128 to 3072; 0 is 3072) is sent as
  `embedContentConfig.outputDimensionality`.
- **Truncation.** A text the API truncated (`truncated: true`) is `ErrRejected`, so `Bisect` sets
  that text aside rather than store a vector of part of it.
- **Tokens.** The meter hears `usageMetadata.promptTokenCount`, the input tokens Google bills.
- **Only `gemini-embedding-2`.** Another model's name is `ErrNoModel`: the others take other
  requests.

```go
tokens := func(ctx context.Context, refresh bool) (string, error) { return myTokens(ctx, refresh) }
p, err := embed.NewVertex(ctx, embed.VertexConfig{Project: "my-project", Location: "us",
	Model: embed.VertexModel, Dimensions: 1536}, tokens, nil)
q, err := embed.Query(ctx, p, "where do we retry a failed embed")
```

## Install

```go
import "github.com/yongjohnlee80/golib/search/embed"
```

## Example

```go
p, err := embed.NewOllama(ctx, embed.DefaultOllamaURL, "", "bge-m3", nil, embed.WithContext(2048))
p.SetMeter(func(c embed.Call) { log.Printf("%d texts, %d tokens, %v", c.Texts, c.Tokens, c.Duration) })

vecs, rejected, err := embed.Bisect(ctx, p, texts, p.Model().Dims, 30*time.Second)
// vecs[i] is normalized, or nil when texts[i] is in rejected: a provider refusing the input
// (too long, say) is narrowed down by halves to the texts it refuses, and the rest are embedded
```

`Models` lists what a provider offers, `OllamaContext` reads a model's context length, and an
Ollama client can `Unload` its model from the server's memory.

`Pull` has an Ollama server pull a model, calling back with its progress summed over the layers;
cancelling its context stops the download unless another client is pulling the same model, and a
later pull resumes it. `Catalog` is the cards of the models measured so far: dims, context,
download size and resident memory at two contexts, each with the digest, Ollama version and device
it was measured on. `Card.Fits` says where one runs within a `Budget` (`FitGPU`, `FitCPU`,
`FitNone`, or `FitUnknown` when it can't tell); a GPU answer keeps `GPUMargin` to spare, since the
cards are measured on a CPU. `ParseFingerprint` reads a `Model.Fingerprint` back.

```go
err := embed.Pull(ctx, embed.DefaultOllamaURL, "", "bge-m3", nil, func(p embed.PullProgress) {
	log.Printf("%s: %d of %d bytes", p.Status, p.Completed, p.Total)
})
for _, c := range embed.Catalog() {
	fmt.Println(c.Name, c.Fits(embed.Budget{VRAM: 8 << 30, RAM: 32 << 30}, 8192))
}
```

| error | when |
| --- | --- |
| `ErrRejected` | the provider refused the input itself (HTTP 400, 413, 422), or truncated it |
| `ErrRateLimited` | its usage limit is reached (429) |
| `ErrUnauthorized` | the key is refused (401, 403) |
| `ErrAPIDisabled` | Vertex AI: the API is not enabled in the project (a 403 saying so; an `ErrUnauthorized` too) |
| `ErrNoModel` | the provider has no such model (Vertex AI: a 404, or a model it doesn't speak) |
| `ErrUnreachable` | nothing answered |
| `ErrDims` | vectors of the wrong size, or the wrong number of them |
| `ErrPullIncomplete` | a pull's stream ended before the server said success |

## License

See [LICENSE](../../LICENSE).
