package linebreak

import (
	"reflect"
	"testing"
)

// A line breaks at white space, and after a CJK character or a hyphen; a no-break space and a
// plain word hold together, and the first unit is never Spaced.
func TestUnitsBreakAtSpacesAndAfterCJKAndHyphens(t *testing.T) {
	for s, want := range map[string][]Unit{
		"":                {},
		"  a  b ":         {{"a", false}, {"b", true}},
		"alpha-beta":      {{"alpha-", false}, {"beta", false}},
		"日本語":             {{"日", false}, {"本", false}, {"語", false}},
		"see 日本 now":      {{"see", false}, {"日", true}, {"本", false}, {"now", true}},
		"a b\tc":          {{"a b", false}, {"c", true}},
		"x- y":            {{"x-", false}, {"y", true}},
		"한국어 カナ":          {{"한", false}, {"국", false}, {"어", false}, {"カ", true}, {"ナ", false}},
		"well-known-word": {{"well-", false}, {"known-", false}, {"word", false}},
	} {
		got := Units(s)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Units(%q) = %v, want %v", s, got, want)
		}
	}
}
