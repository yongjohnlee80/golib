package request

import "encoding/json"

// Error is the default response error representation for HTTP responses
// with non-success (4xx/5xx) status codes, satisfying the [ResponseError] interface.
type Error struct {
	// Status is the HTTP response status code (e.g. 400, 404, 500).
	Status int `json:"status"`
	// Message holds the deserialized error payload (string, map, or struct).
	Message interface{} `json:"message"`
}

// Error formats the error message as a JSON string representation, satisfying
// the standard error interface.
func (e *Error) Error() string {
	str, err := json.Marshal(e.Message)
	if err != nil {
		return ""
	}
	return string(str)
}

// SetStatus records the HTTP status code on the error instance, satisfying
// the [ResponseError] contract during [DecodeResponse].
func (e *Error) SetStatus(status int) {
	e.Status = status
}
