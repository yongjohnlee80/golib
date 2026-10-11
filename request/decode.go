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

// DecodeResponse evaluates the HTTP response status code in p.Response:
//   - On success (HTTP status < 400): unmarshals p.ResponseBody into response (unless empty or nil);
//     returns an unmarshal error if JSON parsing fails.
//   - On client/server error (HTTP status >= 400): unmarshals p.ResponseBody into an error instance
//     of type PT; if unmarshaling fails, a fresh zero-valued error struct is used instead (the unmarshal
//     error is discarded). The status code is recorded via [ResponseError.SetStatus] and returned as an error.
//
// Type parameters:
//   - T: the concrete underlying struct type for the error response.
//   - PT: pointer type to T (*T) constrained to satisfy [ResponseError].
//
// Example:
//
//	var result MyResult
//	if err := request.DecodeResponse[request.Error](params, &result); err != nil {
//	    return fmt.Errorf("request failed: %w", err)
//	}
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
