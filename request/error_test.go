package request

import "testing"

func TestError_Error_StringMessage(t *testing.T) {
	e := &Error{Status: 400, Message: "bad request"}
	got := e.Error()
	if got != `"bad request"` {
		t.Fatalf("expected '\"bad request\"', got %s", got)
	}
}

func TestError_Error_StructMessage(t *testing.T) {
	e := &Error{Status: 422, Message: map[string]string{"field": "required"}}
	got := e.Error()
	if got != `{"field":"required"}` {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestError_Error_NilMessage(t *testing.T) {
	e := &Error{Status: 500, Message: nil}
	got := e.Error()
	if got != "null" {
		t.Fatalf("expected 'null', got %s", got)
	}
}

func TestError_SetStatus(t *testing.T) {
	e := &Error{}
	e.SetStatus(503)
	if e.Status != 503 {
		t.Fatalf("expected 503, got %d", e.Status)
	}
}

func TestError_ImplementsResponseError(t *testing.T) {
	var _ ResponseError = &Error{}
}

func TestError_ImplementsErrorInterface(t *testing.T) {
	var _ error = &Error{}
}