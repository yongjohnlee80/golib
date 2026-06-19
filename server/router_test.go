package server

import (
	"reflect"
	"testing"
)

func mustHandle[H any](t *testing.T, r *Router[H], method, pattern string, h H) {
	t.Helper()
	if err := r.Handle(method, pattern, h); err != nil {
		t.Fatalf("Handle %s %s: %v", method, pattern, err)
	}
}

func TestRouter_StaticParamWildcard(t *testing.T) {
	t.Parallel()
	// H = string proves the router is transport-agnostic (no net/http).
	r := NewRouter[string]()
	mustHandle(t, r, "GET", "/users", "list")
	mustHandle(t, r, "GET", "/users/{id}", "get")
	mustHandle(t, r, "GET", "/files/{rest...}", "files")

	cases := []struct {
		path   string
		want   string
		params map[string]string
	}{
		{"/users", "list", nil},
		{"/users/42", "get", map[string]string{"id": "42"}},
		{"/files/a/b/c.txt", "files", map[string]string{"rest": "a/b/c.txt"}},
	}
	for _, c := range cases {
		res := r.Match("GET", c.path)
		if !res.Found || !res.MethodAllowed {
			t.Fatalf("%s: not matched: %+v", c.path, res)
		}
		if res.Handler != c.want {
			t.Errorf("%s: handler=%q want %q", c.path, res.Handler, c.want)
		}
		if c.params != nil && !reflect.DeepEqual(res.Context.Params(), c.params) {
			t.Errorf("%s: params=%v want %v", c.path, res.Context.Params(), c.params)
		}
	}
}

func TestRouter_StaticBeatsParam(t *testing.T) {
	t.Parallel()
	r := NewRouter[string]()
	mustHandle(t, r, "GET", "/users/{id}", "param")
	mustHandle(t, r, "GET", "/users/me", "static")
	if res := r.Match("GET", "/users/me"); res.Handler != "static" {
		t.Errorf("static should win: %q", res.Handler)
	}
	if res := r.Match("GET", "/users/99"); res.Handler != "param" {
		t.Errorf("param fallback: %q", res.Handler)
	}
}

func TestRouter_NotFoundVsMethodNotAllowed(t *testing.T) {
	t.Parallel()
	r := NewRouter[string]()
	mustHandle(t, r, "GET", "/x", "g")
	mustHandle(t, r, "POST", "/x", "p")

	if res := r.Match("GET", "/nope"); res.Found {
		t.Errorf("expected not-found, got %+v", res)
	}
	res := r.Match("DELETE", "/x")
	if !res.Found || res.MethodAllowed {
		t.Errorf("expected found + method-not-allowed, got %+v", res)
	}
	if !reflect.DeepEqual(res.AllowedMethods, []string{"GET", "POST"}) {
		t.Errorf("AllowedMethods = %v, want [GET POST]", res.AllowedMethods)
	}
}

func TestRouter_Group(t *testing.T) {
	t.Parallel()
	r := NewRouter[string]()
	v1 := r.Group("/api").Group("/v1")
	mustHandle(t, v1, "GET", "/ping", "pong")
	if res := r.Match("GET", "/api/v1/ping"); !res.MethodAllowed || res.Handler != "pong" {
		t.Errorf("group route: %+v", res)
	}
}

func TestRouter_RegistrationErrors(t *testing.T) {
	t.Parallel()
	r := NewRouter[string]()
	if err := r.Handle("", "/x", "h"); err == nil {
		t.Error("empty method should error")
	}
	if err := r.Handle("GET", "/a/{rest...}/b", "h"); err == nil {
		t.Error("non-final wildcard should error")
	}
	mustHandle(t, r, "GET", "/dup", "h")
	if err := r.Handle("GET", "/dup", "h2"); err == nil {
		t.Error("duplicate route should error")
	}
}
