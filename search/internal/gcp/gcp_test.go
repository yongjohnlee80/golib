package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func info(reason, domain, service, consumer string) string {
	return `{"error":{"code":403,"status":"PERMISSION_DENIED","details":[
{"@type":"type.googleapis.com/google.rpc.Help","links":[]},
{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + reason + `","domain":"` + domain +
		`","metadata":{"service":"` + service + `","consumer":"` + consumer + `"}}]}}`
}

// TestDisabled: only an ErrorInfo of SERVICE_DISABLED in googleapis.com names an API, and only
// when its names are Google's identifiers.
func TestDisabled(t *testing.T) {
	cases := []struct {
		name, body, service, consumer string
		ok                            bool
	}{
		{"disabled", info("SERVICE_DISABLED", "googleapis.com", "aiplatform.googleapis.com", "projects/123"), "aiplatform.googleapis.com", "projects/123", true},
		{"no consumer", info("SERVICE_DISABLED", "googleapis.com", "aiplatform.googleapis.com", ""), "aiplatform.googleapis.com", "", true},
		{"a consumer that is text", info("SERVICE_DISABLED", "googleapis.com", "aiplatform.googleapis.com", "see the console"), "aiplatform.googleapis.com", "", true},
		{"another reason", info("IAM_PERMISSION_DENIED", "googleapis.com", "aiplatform.googleapis.com", "projects/1"), "", "", false},
		{"another domain", info("SERVICE_DISABLED", "example.com", "aiplatform.googleapis.com", "projects/1"), "", "", false},
		{"a service that is text", info("SERVICE_DISABLED", "googleapis.com", "enable it at https://x", "projects/1"), "", "", false},
		{"no details", `{"error":{"code":403,"message":"denied"}}`, "", "", false},
		{"not JSON", `denied`, "", "", false},
	}
	for _, c := range cases {
		s, k, ok := Disabled([]byte(c.body))
		if s != c.service || k != c.consumer || ok != c.ok {
			t.Errorf("%s: %q, %q, %v; want %q, %q, %v", c.name, s, k, ok, c.service, c.consumer, c.ok)
		}
	}
}

// TestHeader: the token and the project; a token source's failure passed on, an empty token
// refused, and the refresh asked for passed through.
func TestHeader(t *testing.T) {
	var asked []bool
	h, err := Header(context.Background(), func(_ context.Context, refresh bool) (string, error) {
		asked = append(asked, refresh)
		return "tok", nil
	}, true, "p-1")
	if err != nil || h.Get("Authorization") != "Bearer tok" || h.Get("X-Goog-User-Project") != "p-1" || len(asked) != 1 || !asked[0] {
		t.Errorf("Header = %v, %v; asked %v", h, err, asked)
	}
	boom := errors.New("no credentials")
	if _, err := Header(context.Background(), func(context.Context, bool) (string, error) { return "", boom }, false, "p"); !errors.Is(err, boom) {
		t.Errorf("a failure: %v", err)
	}
	if _, err := Header(context.Background(), func(context.Context, bool) (string, error) { return "", nil }, false, "p"); err == nil ||
		!strings.Contains(err.Error(), "none") {
		t.Errorf("an empty token: %v", err)
	}
}
