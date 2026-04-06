package request

import "encoding/json"

// Error is the default error type for HTTP responses with non-success status codes.
type Error struct {
	Status  int         `json:"status"`
	Message interface{} `json:"message"`
}

func (e *Error) Error() string {
	str, err := json.Marshal(e.Message)
	if err != nil {
		return ""
	}
	return string(str)
}

// SetStatus sets the HTTP status code on the error.
func (e *Error) SetStatus(status int) {
	e.Status = status
}
