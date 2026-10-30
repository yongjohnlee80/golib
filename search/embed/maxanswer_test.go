package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnAnswerPastTheLimitIsRefused: a provider's answer is read to maxAnswer and no further: one
// past it is an error that says so, and one within it decodes.
func TestAnAnswerPastTheLimitIsRefused(t *testing.T) {
	prev := maxAnswer
	maxAnswer = 1 << 10
	defer func() { maxAnswer = prev }()
	big := `{"x":"` + strings.Repeat("a", 4<<10) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			_, _ = w.Write([]byte(big))
			return
		}
		_, _ = w.Write([]byte(`{"x":"small"}`))
	}))
	defer srv.Close()
	var out struct{ X string }
	if err := call(context.Background(), srv.Client(), http.MethodPost, srv.URL+"/big", nil, map[string]any{}, &out); err == nil || !strings.Contains(err.Error(), "over 1024 bytes") {
		t.Errorf("a 4 KiB answer past a 1 KiB limit: %v", err)
	}
	if err := call(context.Background(), srv.Client(), http.MethodPost, srv.URL+"/small", nil, map[string]any{}, &out); err != nil || out.X != "small" {
		t.Errorf("an answer within the limit: %v %q", err, out.X)
	}
}
