// Package request is a small HTTP client: Request/Do execute a request/response
// cycle into a Params carrier (transport errors only — status codes are data),
// DecodeResponse maps the result into typed success or error values, FormWriter
// builds multipart payloads, and Histories keeps a bounded debug trail of
// recent request/response pairs.
package request
