package query

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

func TestTSQuery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		terms []Term
		want  string
	}{
		{[]Term{{Text: "alpha"}}, `'alpha'`},
		{[]Term{{Text: "alpha"}, {Text: "gui", Prefix: true}}, `'alpha' & 'gui':*`},
		{[]Term{{Text: "o'brien"}}, `'o''brien'`},
		{[]Term{{Text: `a\b`}}, `'a\\b'`},
		{[]Term{{Text: "rock&roll"}, {Text: "a:b"}, {Text: "x|y"}, {Text: "!(z)"}}, `'rock&roll' & 'a:b' & 'x|y' & '!(z)'`},
		{[]Term{{Text: `x' | 'y`}}, `'x'' | ''y'`},
		{[]Term{{Text: `\'`}}, `'\\'''`},
	} {
		got, err := TSQuery(tc.terms)
		if err != nil || got != tc.want {
			t.Errorf("%v: %s %v, want %s", tc.terms, got, err, tc.want)
		}
	}
	if _, err := TSQuery(nil); !errors.Is(err, ErrNoTerms) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("no terms: %v", err)
	}
}
