package request

import (
	"encoding/json"
)

// ResponseError is a constraint for error types used with DecodeResponse.
// Implementations must provide the standard error interface and a way to
// set the HTTP status code.
type ResponseError interface {
	error
	SetStatus(int)
}

// DecodeResponse checks the HTTP status code and either unmarshals the response
// body into `response` on success, or unmarshals into the error type E on failure.
//
// T is the underlying struct type, PT is its pointer that implements ResponseError.
// Usage: DecodeResponse[MyError](p, &resp) where *MyError implements ResponseError.
func DecodeResponse[T any, PT interface {
	*T
	ResponseError
}](p *Params, response any) error {
	if p.Response.StatusCode >= 400 {
		errResp := PT(new(T))
		if err := json.Unmarshal([]byte(p.ResponseBody), errResp); err != nil {
			errResp = PT(new(T))
		}
		errResp.SetStatus(p.Response.StatusCode)
		return errResp
	}
	// A success response with no body (204 No Content, empty 200) is not a
	// decode error; the response target is simply left untouched.
	if len(p.ResponseBody) == 0 || response == nil {
		return nil
	}
	return json.Unmarshal([]byte(p.ResponseBody), response)
}
