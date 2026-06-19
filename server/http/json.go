package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// DefaultMaxBodyBytes bounds Decode when the caller passes maxBytes <= 0.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// JSON writes v as a JSON response with the given status. It sets the
// Content-Type and returns any encode error (after the header is committed).
func JSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// errorEnvelope is the JSON shape for Error. Its tags are schema-compatible with
// golib/request.Error ({"status":int,"message":any}), so a request client can
// decode a server error directly into request.Error.
type errorEnvelope struct {
	Status  int `json:"status"`
	Message any `json:"message"`
}

// Error writes a JSON error envelope: {"status":<status>,"message":<message>}.
// message is typically a string but may be any JSON-serialisable value.
func Error(w http.ResponseWriter, status int, message any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Status: status, Message: message})
}

// Decode reads and JSON-decodes the request body into a T, rejecting unknown
// fields and bounding the body to maxBytes (DefaultMaxBodyBytes when <= 0).
func Decode[T any](r *http.Request, maxBytes int64) (T, error) {
	var v T
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}

// --- error-returning handler adapter ---------------------------------------

// HandlerFunc is an error-returning handler. Adapt it with Handler so a returned
// error becomes a JSON response: a *StatusError maps to its status/message, any
// other error to 500.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// StatusError carries an HTTP status + JSON message for the Handler adapter.
type StatusError struct {
	Status  int
	Message any
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("status %d: %v", e.Status, e.Message)
}

// Status builds a *StatusError (sentinel-style: return Status(404, "no user")).
func Status(status int, message any) *StatusError {
	return &StatusError{Status: status, Message: message}
}

// Handler adapts an error-returning HandlerFunc to a net/http handler.
func Handler(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}
		var se *StatusError
		if errors.As(err, &se) {
			Error(w, se.Status, se.Message)
			return
		}
		Error(w, http.StatusInternalServerError, "internal server error")
	}
}
