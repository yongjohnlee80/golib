package js

import "testing"

func TestNewParsers_MatchTheLegacyFields(t *testing.T) {
	t.Parallel()
	d := &ExprDialect{Name: "test"}
	if got, want := NewExpression(WithDialect(d), MaxDepth(7), nil), (Expression{Dialect: d, MaxDepth: 7}); got != want {
		t.Errorf("NewExpression = %+v, want %+v", got, want)
	}
	if got, want := NewStatements(WithDialect(d), MaxDepth(7)), (Statements{Dialect: d, MaxDepth: 7}); got != want {
		t.Errorf("NewStatements = %+v, want %+v", got, want)
	}
	if got := NewExpression(); got != (Expression{}) {
		t.Errorf("NewExpression() = %+v, want the zero value", got)
	}
	// The depth bound reaches the parser: a nesting deeper than the bound is refused.
	if _, err := NewExpression(MaxDepth(2)).Parse([]byte("((((1))))")); err == nil {
		t.Error("MaxDepth(2) accepted four levels of parentheses")
	}
}
