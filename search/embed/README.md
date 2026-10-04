# search/embed — embedding providers over plain HTTP

A `Provider` turns texts into vectors: `Name`, `Model` (whose `Fingerprint` names it exactly) and
`Embed`. `NewOllama` speaks Ollama's `/api/embed`, local or Ollama Cloud. `NewOpenAI` speaks any
OpenAI-compatible `/v1/embeddings`. Both use `net/http`, with no SDK. Each probes the model when it
is made, so a missing model fails at setup rather than at the first document.

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

| error | when |
| --- | --- |
| `ErrRejected` | the provider refused the input itself (HTTP 400, 413, 422) |
| `ErrRateLimited` | its usage limit is reached (429) |
| `ErrUnauthorized` | the key is refused (401, 403) |
| `ErrNoModel` | the provider has no such model |
| `ErrUnreachable` | nothing answered |
| `ErrDims` | vectors of the wrong size, or the wrong number of them |

## License

See [LICENSE](../../LICENSE).
