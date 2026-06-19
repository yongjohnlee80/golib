package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/request"
)

func TestJSON(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	if err := JSON(rec, http.StatusCreated, map[string]int{"n": 7}); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("code=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type=%q", ct)
	}
	var got map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got["n"] != 7 {
		t.Errorf("body=%s err=%v", rec.Body.Bytes(), err)
	}
}

// TestError_RequestInterop proves the Error envelope decodes straight into
// request.Error (same JSON schema {status,message}).
func TestError_RequestInterop(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	Error(rec, http.StatusNotFound, "no such user")
	if rec.Code != http.StatusNotFound {
		t.Errorf("code=%d", rec.Code)
	}
	var re request.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &re); err != nil {
		t.Fatalf("decode into request.Error: %v (%s)", err, rec.Body.Bytes())
	}
	if re.Status != http.StatusNotFound {
		t.Errorf("status=%d", re.Status)
	}
	if re.Message != "no such user" {
		t.Errorf("message=%v", re.Message)
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()
	type payload struct {
		Name string `json:"name"`
	}

	// valid
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ada"}`))
	v, err := Decode[payload](r, 0)
	if err != nil || v.Name != "ada" {
		t.Fatalf("valid decode: v=%+v err=%v", v, err)
	}

	// unknown field rejected
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","extra":1}`))
	if _, err := Decode[payload](r, 0); err == nil {
		t.Error("expected unknown-field error")
	}

	// oversize body rejected by maxBytes
	big := `{"name":"` + strings.Repeat("a", 200) + `"}`
	r = httptest.NewRequest("POST", "/", strings.NewReader(big))
	if _, err := Decode[payload](r, 16); err == nil {
		t.Error("expected max-bytes error for oversize body")
	}
}

func TestHandler_StatusError(t *testing.T) {
	t.Parallel()

	// *StatusError maps to its status/message.
	rec := httptest.NewRecorder()
	Handler(func(http.ResponseWriter, *http.Request) error {
		return Status(http.StatusForbidden, "denied")
	})(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status err code=%d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("denied")) {
		t.Errorf("body=%s", rec.Body.Bytes())
	}

	// a plain error becomes 500.
	rec = httptest.NewRecorder()
	Handler(func(http.ResponseWriter, *http.Request) error {
		return errors.New("boom")
	})(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("plain err code=%d", rec.Code)
	}

	// nil error: handler's own write stands, no override.
	rec = httptest.NewRecorder()
	Handler(func(w http.ResponseWriter, _ *http.Request) error {
		return JSON(w, http.StatusOK, "fine")
	})(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("nil err code=%d", rec.Code)
	}
}
