package server

import "testing"

func TestRouteContext(t *testing.T) {
	t.Parallel()

	// nil-safe.
	var nilrc *RouteContext
	if nilrc.Param("x") != "" {
		t.Error("nil Param should be empty")
	}
	if len(nilrc.Params()) != 0 {
		t.Error("nil Params should be empty")
	}

	rc := &RouteContext{params: map[string]string{"id": "7"}}
	if rc.Param("id") != "7" {
		t.Errorf("Param(id) = %q, want 7", rc.Param("id"))
	}
	if rc.Param("nope") != "" {
		t.Error("absent param should be empty")
	}
	// Params() is a defensive copy.
	cp := rc.Params()
	cp["id"] = "mutated"
	if rc.Param("id") != "7" {
		t.Error("mutating Params() result must not affect routing state")
	}
}
