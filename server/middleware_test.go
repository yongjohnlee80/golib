package server

import "testing"

func TestChain_Order(t *testing.T) {
	t.Parallel()
	a := func(s string) string { return "a>" + s }
	b := func(s string) string { return "b>" + s }
	// First-Use'd is outermost (leftmost in the result).
	if got := NewChain[string]().Use(a).Use(b).Then("h"); got != "a>b>h" {
		t.Errorf("order = %q, want a>b>h", got)
	}
}

func TestChain_NoAlias(t *testing.T) {
	t.Parallel()
	add := func(tag string) Middleware[string] {
		return func(s string) string { return tag + s }
	}
	base := NewChain[string](add("base|"))
	g1 := base.Use(add("g1|"))
	g2 := base.Use(add("g2|"))

	if got := base.Then("X"); got != "base|X" {
		t.Errorf("base mutated: %q", got)
	}
	if got := g1.Then("X"); got != "base|g1|X" {
		t.Errorf("g1 = %q", got)
	}
	if got := g2.Then("X"); got != "base|g2|X" {
		t.Errorf("g2 = %q (g1/g2 leaked into each other)", got)
	}
}

func TestChain_When(t *testing.T) {
	t.Parallel()
	add := func(s string) string { return "x>" + s }
	if got := NewChain[string]().When(true, add).Then("h"); got != "x>h" {
		t.Errorf("When(true) = %q", got)
	}
	if got := NewChain[string]().When(false, add).Then("h"); got != "h" {
		t.Errorf("When(false) = %q", got)
	}
}

func TestChain_ExtendLen(t *testing.T) {
	t.Parallel()
	id := func(s string) string { return s }
	c := NewChain[string](id, id).Extend(NewChain[string](id))
	if c.Len() != 3 {
		t.Errorf("Len = %d, want 3", c.Len())
	}
}
