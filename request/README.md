# request

A lightweight HTTP client: run a request/response cycle into a `Params`
carrier (transport errors only — status codes are data), map the result into
typed success or error values, build multipart payloads, and keep a bounded
debug trail of recent exchanges. Zero external dependencies.

```bash
go get github.com/yongjohnlee80/golib/request
```

```go
import "github.com/yongjohnlee80/golib/request"
```

## Contents

- [Basic requests](#basic-requests) · [Context & timeouts](#context--timeouts)
- [Payload types](#payload-types) · [Decoding responses](#decoding-responses)
- [Options](#request-options) · [Multipart forms](#multipart-forms)
- [Response size limit](#response-size-limit) · [History](#request-history)
- [Extending](#extending) · [Gotchas](#gotchas) · [File layout](#file-layout)

---

## Basic requests

`Request` executes an HTTP request and populates the `Params` struct with the
outcome. **Only transport-level errors are returned** — a 4xx/5xx is not an
error, it is data on `p.Response`/`p.ResponseBody`. Use
[`DecodeResponse`](#decoding-responses) to turn a status into a typed error.

```go
p := &request.Params{
    Method: "POST",
    Url:    "https://api.example.com/tracks",
    Headers: map[string]string{"Authorization": "Bearer " + token},
}
if err := request.Request(p, payload); err != nil {
    return err // network/DNS/timeout — not an HTTP status
}
// p.Response.StatusCode and p.ResponseBody are now populated.
```

The one-line convenience helpers `Get` and `Post` build the `Params`, run the
request, and decode in one call using the default [`Error`](#the-default-error-type)
type. They use `DefaultTimeout`; build `Params` yourself when you need a
different timeout.

```go
resp, err := request.Get("https://api.example.com/items",
    map[string][]string{"page": {"1"}}, &result) // query-encoded
resp, err := request.Post("https://api.example.com/items", payload, &result)
```

`StatusCodeInBounds(code)` reports whether a status is in the 2xx–3xx range.

## Context & timeouts

`Do` is the context-aware form — cancelling `ctx` aborts both the request and
the response-body read:

```go
err := request.Do(ctx, p, payload)
```

`Params.Timeout` is a real `time.Duration` bounding the whole cycle:

| `Timeout` value | Effect |
|---|---|
| `0` (zero value) | `DefaultTimeout` (10s) |
| `30 * time.Second` | 30-second deadline |
| negative | no timeout |

```go
p := &request.Params{Method: "GET", Url: u, Timeout: 30 * time.Second}
```

Requests share a package-global `http.Transport`, so connection pooling and
keep-alive work across calls automatically.

## Payload types

The `payload` argument is dispatched by its Go type:

| Type | Behavior |
|---|---|
| `CustomPayload` | Used as an `io.Reader`; its `ContentType()` sets the header |
| `io.Reader` | Sent as-is, no content-type set |
| `url.Values` | Form-encoded, `application/x-www-form-urlencoded` |
| anything else | JSON-marshalled, `application/json; charset=UTF-8` |
| `nil` | no body |

An explicit `Headers["Content-Type"]` (or the [`ContentType`](#request-options)
option) always wins over the payload-derived type. Headers are applied with
`Header.Set`, so there is exactly one value per header.

## Decoding responses

`DecodeResponse` is generic over the error type. On status `>= 400` it
unmarshals the body into your error type and returns it as an `error`;
otherwise it unmarshals into `response` (an empty 2xx body or a nil target is
a no-op, not an error).

```go
// Default Error type:
err := request.DecodeResponse[request.Error](p, &response)

// Custom error type — *MyAPIError must implement ResponseError:
err := request.DecodeResponse[MyAPIError](p, &response)
```

Custom error types implement `ResponseError`:

```go
type ResponseError interface {
    error
    SetStatus(int)
}
```

### The default Error type

```go
type Error struct {
    Status  int         `json:"status"`
    Message interface{} `json:"message"`
}
```

## Request options

`RequestOption` values mutate `Params` before the request is sent:

```go
err := request.Request(p, payload,
    request.ContentType(request.JSON), // or request.MULTIPART
    WithAuth(token),
)
```

Content-type constants: `request.JSON`, `request.MULTIPART`; and the raw
strings `request.TypeJSON` / `request.TypeJSONUTF8` for header values.

Define your own option — it is just `func(*Params) *Params`:

```go
func WithAuth(token string) request.RequestOption {
    return func(p *request.Params) *request.Params {
        if p.Headers == nil {
            p.Headers = map[string]string{}
        }
        p.Headers["Authorization"] = "Bearer " + token
        return p
    }
}
```

## Multipart forms

`FormWriter` builds a `multipart/form-data` payload and implements
`CustomPayload`, so it is passed directly to `Request`. **Call `Close` before
sending** — it finalizes the multipart boundary.

```go
fw := request.NewFormWriter()
fw.WriteFile("track", &request.FileUpload{FileName: "song.mp3", Content: file})
fw.WriteFields(metadata)        // struct → fields via json tags
fw.WriteField("note", "hello")  // single string field
fw.Close()

err := request.Request(&request.Params{Method: "POST", Url: u}, fw)
```

`WriteFields` encodes struct fields keyed by their `json` tag. It supports
`string`, `int*`/`uint*`/`float*`, `bool` (as `"1"`/`"0"`), slices (appended
with a `[]` suffix), and pointers (nil skipped). Zero values are omitted, and
fields without a `json` tag or tagged `json:"-"` are skipped.

```go
type TrackMeta struct {
    Title string   `json:"title"`
    BPM   int      `json:"bpm"`
    Tags  []string `json:"tags"`
}
```

`FileUpload.FileName` may be a full path; its basename becomes the form
filename. For hand-rolled multipart bodies, the package-level `WriteFile` /
`WriteFields` accept a raw `*multipart.Writer`; `FormWriter.Writer()` exposes
the underlying writer (e.g. for `CreatePart`), and `FormWriter.Boundary()`
returns the boundary.

> `FormWriter.WriteField(key, value string)` is a string convenience; the
> package-level `WriteField(w, key, reflect.Value)` is the reflection-based
> primitive `WriteFields` builds on.

## Response size limit

Bodies are capped at `MaxResponseBodySize` (10 MB) by default. Override
per-request via `Params.MaxResponseSize` (`0` = default, `-1` = unlimited).
Exceeding the cap returns `ErrResponseTooLarge`:

```go
p.MaxResponseSize = 50 << 20 // 50 MB
if err := request.Request(p, nil); errors.Is(err, request.ErrResponseTooLarge) {
    // body was larger than the cap
}
```

## Request history

`Histories` is a bounded, concurrency-safe ring buffer of recent
request/response pairs for debugging:

```go
h := request.NewHistories(5) // keep the last 5 (0 → default 3)

request.Request(p, payload)
h.Add(p) // captures method, url, copied headers, request body, response

entry := h.GetHistory(1) // 1 = most recent; nil when empty; index is clamped
```

Request bodies over 64 KB are truncated (and flagged) in the stored entry, and
the original body is restored so downstream reads still see it. Headers are
deep-copied at capture time.

> `HistoryEntry` and its fields are **opaque** (no exported accessors): the
> buffer is a capture/inspect-in-a-debugger aid, not a queryable log. Add
> accessors in the package if you need programmatic reads.

## Extending

- **Custom payload encodings** — implement `CustomPayload` (`io.Reader` +
  `ContentType() string`) and pass the value straight to `Request`;
  `FormWriter` is the reference implementation.
- **Custom error envelopes** — implement `ResponseError` and use
  `DecodeResponse[YourError]`.
- **Cross-cutting request mutation** (auth, tracing headers, user-agent) —
  write a `RequestOption`.

## Gotchas

- A 4xx/5xx is **not** a returned error — check `p.Response.StatusCode` or use
  `DecodeResponse`.
- `Get`/`Post` always use `DefaultTimeout`; construct `Params` for a custom
  one.
- `Request` has no cancellation; use `Do(ctx, …)` for deadlines/cancellation.
- `FormWriter` must be `Close`d before it is sent.

## File layout

| File | Contents |
|---|---|
| `request.go` | `Params`, `Request`, `Do`, `Get`, `Post`, `CustomPayload`, `StatusCodeInBounds`, `ErrResponseTooLarge`, size/timeout constants |
| `decode.go` | `ResponseError`, `DecodeResponse[T]` |
| `error.go` | default `Error` type |
| `options.go` | `RequestOption`, `ContentType`, content-type constants |
| `multipart.go` | `FormWriter` |
| `form.go` | `FileUpload`, `WriteFile`, `WriteFields`, `WriteField` |
| `history.go` | `Histories`, `HistoryEntry` |

## License

See [LICENSE](../LICENSE).