# request

A lightweight HTTP client package with generic error handling, functional options, multipart form support, and request history tracking.

```bash
go get github.com/yongjohnlee80/golib/request
```

## Core API

### Request

`Request` executes an HTTP request and populates the `Params` struct with the response. Only transport-level errors are returned — HTTP status codes (4xx, 5xx) are **not** treated as errors. Use `DecodeResponse` to handle those.

```go
p := &request.Params{
    Method:  "POST",
    Url:     "https://api.example.com/tracks",
    Timeout: 30,
    Headers: map[string]string{
        "Authorization": "Bearer token",
    },
}

err := request.Request(p, payload)
// p.Response.StatusCode, p.ResponseBody are now populated
```

#### Payload types

The `payload` argument is dispatched by type:

| Type | Behavior |
|---|---|
| `CustomPayload` | Uses the reader directly with `ContentType()` header |
| `io.Reader` | Sent as-is, no content-type set |
| `url.Values` | Form-encoded with `application/x-www-form-urlencoded` |
| Any other value | JSON-marshalled with `application/json; charset=UTF-8` |

#### Response body size limit

By default, response bodies are capped at 10 MB (`MaxResponseBodySize`). Override per-request:

```go
p := &request.Params{
    MaxResponseSize: 50 << 20, // 50 MB
    // ...
}

// Or disable the limit entirely
p.MaxResponseSize = -1
```

### Convenience functions

```go
// GET with query params, decodes response or returns *Error
resp, err := request.Get("https://api.example.com/items", map[string][]string{
    "page": {"1"},
}, &result)

// POST with JSON payload
resp, err := request.Post("https://api.example.com/items", payload, &result)
```

## Generic Error Handling

### DecodeResponse

`DecodeResponse` is generic over the error type. For status >= 400, it unmarshals the body into your error type; otherwise it unmarshals into the response.

```go
// Using the default Error type
err := request.DecodeResponse[request.Error](p, &response)

// Using a custom error type
err := request.DecodeResponse[MyAPIError](p, &response)
```

Custom error types must implement `ResponseError`:

```go
type ResponseError interface {
    error
    SetStatus(int)
}
```

### Default Error

The built-in `Error` type works out of the box:

```go
type Error struct {
    Status  int         `json:"status"`
    Message interface{} `json:"message"`
}
```

## Request Options

Functional options modify `Params` before execution:

```go
err := request.Request(p, payload,
    request.ContentType(request.JSON),
    myCustomOption,
)
```

Built-in options:

```go
request.ContentType(request.JSON)      // application/json
request.ContentType(request.MULTIPART) // multipart/form-data
```

Define your own:

```go
func WithAuth(token string) request.RequestOption {
    return func(p *request.Params) *request.Params {
        if p.Headers == nil {
            p.Headers = make(map[string]string)
        }
        p.Headers["Authorization"] = "Bearer " + token
        return p
    }
}
```

## Multipart Forms

### FormWriter

`FormWriter` is a builder for multipart payloads. It implements `CustomPayload`, so it can be passed directly to `Request`.

```go
fw := request.NewFormWriter()
fw.WriteFile("track", &request.FileUpload{
    FileName: "song.mp3",
    Content:  file,
})
fw.WriteFields(metadata)          // struct → form fields via json tags
fw.WriteField("extra", "value")   // single field
fw.Close()

p := &request.Params{Method: "POST", Url: url}
err := request.Request(p, fw)
```

### WriteFields

Encodes struct fields into multipart form fields using JSON tags. Supports `string`, `int*`, `uint*`, `float*`, `bool` (1/0), slices (appended with `[]` suffix), and pointers. Zero values are omitted. Fields without a `json` tag or tagged `json:"-"` are skipped.

```go
type TrackMeta struct {
    Title  string   `json:"title"`
    BPM    int      `json:"bpm"`
    Tags   []string `json:"tags"`
    Active bool     `json:"active"`
}
```

### FileUpload

```go
type FileUpload struct {
    FileName string    // full path or name; basename is used in the form
    Content  io.Reader
}
```

### Standalone functions

`WriteFile` and `WriteFields` also accept a raw `*multipart.Writer` for advanced use cases:

```go
body := &bytes.Buffer{}
w := multipart.NewWriter(body)
request.WriteFile(w, "track", file)
request.WriteFields(w, meta)
w.Close()
```

## Request History

Track recent HTTP request/response pairs for debugging:

```go
h := request.NewHistories(5) // keep last 5

// After each request:
request.Request(p, payload)
h.Add(p) // captures method, url, headers, request body, response

// Retrieve (1 = most recent):
entry := h.GetHistory(1)
```

Request bodies larger than 64 KB are truncated. Headers are copied on capture, so later mutations don't affect history entries.

## File Layout

| File | Contents |
|---|---|
| `request.go` | `Params`, `Request()`, `Get()`, `Post()`, `CustomPayload` |
| `decode.go` | `ResponseError`, `DecodeResponse[T]()` |
| `error.go` | Default `Error` type |
| `options.go` | `RequestOption`, `ContentType()`, content-type constants |
| `multipart.go` | `FormWriter` |
| `form.go` | `FileUpload`, `WriteFile()`, `WriteFields()`, `WriteField()` |
| `history.go` | `Histories`, `HistoryEntry` |