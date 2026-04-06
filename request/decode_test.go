package request

import (
	"net/http"
	"testing"
)

// CustomError mimics a domain-specific error type like aims.Error.
type CustomError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
}

func (e *CustomError) Error() string { return e.Message }
func (e *CustomError) SetStatus(s int) { e.Status = s }

func makeParams(statusCode int, body string) *Params {
	return &Params{
		Response:     &http.Response{StatusCode: statusCode},
		ResponseBody: body,
	}
}

// ---------------------------------------------------------------------------
// DecodeResponse with default Error
// ---------------------------------------------------------------------------

func TestDecodeResponse_DefaultError_Success(t *testing.T) {
	p := makeParams(200, `{"name":"alice"}`)
	var resp struct{ Name string }
	err := DecodeResponse[Error](p, &resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "alice" {
		t.Fatalf("expected alice, got %s", resp.Name)
	}
}

func TestDecodeResponse_DefaultError_400(t *testing.T) {
	p := makeParams(400, `{"message":"bad request"}`)
	var resp any
	err := DecodeResponse[Error](p, &resp)
	if err == nil {
		t.Fatal("expected error")
	}
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.Status != 400 {
		t.Fatalf("expected status 400, got %d", e.Status)
	}
	if e.Error() != `"bad request"` {
		t.Fatalf("unexpected message: %s", e.Error())
	}
}

func TestDecodeResponse_DefaultError_500(t *testing.T) {
	p := makeParams(500, `{"message":"internal server error"}`)
	var resp any
	err := DecodeResponse[Error](p, &resp)
	if err == nil {
		t.Fatal("expected error")
	}
	e := err.(*Error)
	if e.Status != 500 {
		t.Fatalf("expected 500, got %d", e.Status)
	}
}

func TestDecodeResponse_DefaultError_InvalidJSON(t *testing.T) {
	p := makeParams(502, `not json at all`)
	var resp any
	err := DecodeResponse[Error](p, &resp)
	if err == nil {
		t.Fatal("expected error for 502")
	}
	e := err.(*Error)
	if e.Status != 502 {
		t.Fatalf("expected 502, got %d", e.Status)
	}
}

func TestDecodeResponse_DefaultError_301(t *testing.T) {
	p := makeParams(301, `{"location":"somewhere"}`)
	var resp map[string]string
	err := DecodeResponse[Error](p, &resp)
	if err != nil {
		t.Fatalf("unexpected error for 3xx: %v", err)
	}
	if resp["location"] != "somewhere" {
		t.Fatal("expected response to decode for 301")
	}
}

// ---------------------------------------------------------------------------
// DecodeResponse with custom error type
// ---------------------------------------------------------------------------

func TestDecodeResponse_CustomError_Success(t *testing.T) {
	p := makeParams(200, `{"id":"123"}`)
	var resp struct{ ID string }
	err := DecodeResponse[CustomError](p, &resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "123" {
		t.Fatalf("expected 123, got %s", resp.ID)
	}
}

func TestDecodeResponse_CustomError_ErrorResponse(t *testing.T) {
	p := makeParams(422, `{"code":3001,"message":"validation failed"}`)
	var resp any
	err := DecodeResponse[CustomError](p, &resp)
	if err == nil {
		t.Fatal("expected error")
	}
	ce, ok := err.(*CustomError)
	if !ok {
		t.Fatalf("expected *CustomError, got %T", err)
	}
	if ce.Code != 3001 {
		t.Fatalf("expected code 3001, got %d", ce.Code)
	}
	if ce.Message != "validation failed" {
		t.Fatalf("expected 'validation failed', got %s", ce.Message)
	}
	if ce.Status != 422 {
		t.Fatalf("expected status 422, got %d", ce.Status)
	}
}

func TestDecodeResponse_CustomError_InvalidJSON(t *testing.T) {
	p := makeParams(500, `<html>error</html>`)
	var resp any
	err := DecodeResponse[CustomError](p, &resp)
	if err == nil {
		t.Fatal("expected error")
	}
	ce := err.(*CustomError)
	if ce.Status != 500 {
		t.Fatalf("expected status 500, got %d", ce.Status)
	}
	// Fields should be zero-valued since unmarshal failed and a fresh instance was created.
	if ce.Code != 0 {
		t.Fatalf("expected code 0 on unmarshal failure, got %d", ce.Code)
	}
}

func TestDecodeResponse_CustomError_SetStatusOverwritesBody(t *testing.T) {
	// Even if the JSON body contains a different status, SetStatus should override.
	p := makeParams(503, `{"code":1001,"message":"unavailable","status":0}`)
	var resp any
	err := DecodeResponse[CustomError](p, &resp)
	ce := err.(*CustomError)
	if ce.Status != 503 {
		t.Fatalf("SetStatus should set 503, got %d", ce.Status)
	}
}

func TestDecodeResponse_InvalidSuccessJSON(t *testing.T) {
	p := makeParams(200, `not json`)
	var resp map[string]string
	err := DecodeResponse[Error](p, &resp)
	if err == nil {
		t.Fatal("expected unmarshal error for invalid success body")
	}
}

// ---------------------------------------------------------------------------
// Boundary: status 399 vs 400
// ---------------------------------------------------------------------------

func TestDecodeResponse_Boundary399(t *testing.T) {
	p := makeParams(399, `{"ok":true}`)
	var resp struct{ Ok bool }
	err := DecodeResponse[Error](p, &resp)
	if err != nil {
		t.Fatalf("399 should be treated as success, got: %v", err)
	}
	if !resp.Ok {
		t.Fatal("expected ok=true")
	}
}

func TestDecodeResponse_Boundary400(t *testing.T) {
	p := makeParams(400, `{"message":"bad"}`)
	var resp any
	err := DecodeResponse[Error](p, &resp)
	if err == nil {
		t.Fatal("400 should be treated as error")
	}
}