package html_test

import (
	"sort"
	"testing"
)

func allCases() []specCase {
	var out []specCase
	for _, cs := range [][]specCase{casesPreliminaries, casesLeaf, casesContainer, casesInline} {
		out = append(out, cs...)
	}
	return out
}

// TestRuleCoverage fails unless every rule in the checklist has at least one positive and one
// negative case (or a stated NoNegative reason), and every case names a rule that exists.
func TestRuleCoverage(t *testing.T) {
	pos, neg := map[string]int{}, map[string]int{}
	known := map[string]bool{}
	for _, r := range rules {
		if known[r.ID] {
			t.Errorf("rule %s is listed twice", r.ID)
		}
		known[r.ID] = true
	}
	for _, c := range allCases() {
		if !known[c.Rule] {
			t.Errorf("case for unknown rule %q (in: %q)", c.Rule, c.In)
		}
		if c.Pos {
			pos[c.Rule]++
		} else {
			neg[c.Rule]++
		}
	}
	var exempt []string
	for _, r := range rules {
		if pos[r.ID] == 0 {
			t.Errorf("rule %s has no positive case", r.ID)
		}
		if neg[r.ID] == 0 {
			if r.NoNegative == "" {
				t.Errorf("rule %s has no negative case and no NoNegative reason", r.ID)
			} else {
				exempt = append(exempt, r.ID+": "+r.NoNegative)
			}
		}
	}
	sort.Strings(exempt)
	for _, e := range exempt {
		t.Logf("no negative case, by exemption — %s", e)
	}
	t.Logf("CommonMark %s: %d rules, %d cases", SpecVersion, len(rules), len(allCases()))
}
