package request

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A nil option is no option: Do skips it, as every constructor in golib does, and the
// options after it still apply.
func TestDoSkipsANilOption(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Do with a nil option panicked: %v", r)
		}
	}()
	applied := false
	p := &Params{Method: "GET", Url: srv.URL}
	if err := Do(context.Background(), p, nil, nil, func(p *Params) *Params { applied = true; return p }); err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Error("the option after the nil was not applied")
	}
}
