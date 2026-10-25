package sql

import "testing"

// Each option sets exactly the field it names, and nothing else.
func TestEachOptionSetsItsOwnField(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		opts []Option
		want SQL
	}{
		"none":     {nil, SQL{}},
		"mysql":    {[]Option{Backticks()}, SQL{backticks: true}},
		"postgres": {[]Option{DollarQuotes(), NestedBlockComments(), EStringEscapes()}, SQL{dollarQuotes: true, nestedBlockComments: true, eStringEscapes: true}},
		"sqlite":   {[]Option{TriggerBodies(), nil}, SQL{triggerBodies: true}},
	} {
		if got := New(tc.opts...); got != tc.want {
			t.Errorf("%s: New = %+v, want %+v", name, got, tc.want)
		}
	}
}
