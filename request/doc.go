// Package request provides a lightweight HTTP client and request/response orchestration
// toolkit for golib services. It streamlines outbound HTTP interactions with sensible
// defaults, bounded memory consumption, and structured error handling.
//
// # Core Features
//
//   - Execution Lifecycle: [Request] and [Do] execute HTTP requests using a shared connection-pooled
//     transport. Transport errors are returned as Go errors, while HTTP status codes (such as 4xx
//     and 5xx) are captured as data for deterministic inspection.
//   - Parameter Carrier: [Params] encapsulates request parameters (URL, HTTP method, headers,
//     cookies, timeout) and holds the resulting response state ([http.Response], response body string).
//   - Response Decoding: [DecodeResponse] inspects the HTTP response status, deserializing successful
//     payloads into target structs and mapping 4xx/5xx responses into typed errors implementing [ResponseError].
//   - Multipart Forms: [FormWriter] and [WriteFields] provide multipart/form-data encoding for file
//     uploads ([FileUpload]) and structured metadata.
//   - Request History: [Histories] maintains a concurrency-safe, bounded ring buffer of recent request
//     and response cycles for debugging, inspection, and test verification.
//   - Bounded Resource Protection: Enforces [MaxResponseBodySize] (default 10 MB) to protect against
//     memory exhaustion from unexpected or malicious upstream payloads.
package request
