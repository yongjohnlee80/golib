package audit

import (
	"sort"
	"strings"
	"testing"
)

// Fixtures for every pointer detector.
//
// A detector that matches nothing reports its whole class as CLEAN, which is
// worse than not having it: an absent instrument gets attention, a present and
// blind one turns "nobody checked" into "checked and fine". The first version
// of this budget was blind to bare review coordinates and to criterion
// numbers, and reported a repository-wide total that was simply too low.
//
// So each detector must be shown to fire on something, and — just as
// important — to stay silent on a lookalike. Sensitivity without specificity
// would let a detector pass by matching everything.
var detectorFixtures = map[string]struct {
	positive []string // must be counted, and by THIS detector
	negative []string // must be counted by NO detector
}{
	"design-record-number": {
		positive: []string{"// see ADR-0013 for the rule", "// ADR 0075 says otherwise"},
		negative: []string{"// the ADR process is documented elsewhere", "// ADRIAN wrote this"},
	},
	"design-record-slug": {
		positive: []string{"// golib-dao-0020 covers this"},
		negative: []string{"// golib-dao is the package family"},
	},
	"section-anchor": {
		positive: []string{"// the rule in §2.3"},
		negative: []string{"// section 2 of this file"},
	},
	"review-round": {
		positive: []string{"// folded MF2 during the pass", "// nit 4 asked for this"},
		negative: []string{"// MFA is not this", "// the knit is intentional"},
	},
	"reviewer-as-authority": {
		positive: []string{"// gold-man asked for this"},
		negative: []string{"// the gold path is the happy one"},
	},
	"amendment-number": {
		positive: []string{"// Amendment 6 applies here"},
		negative: []string{"// amendments are tracked elsewhere"},
	},
	"pull-request-number": {
		positive: []string{"// fixed in PR #34"},
		negative: []string{"// see issue tracker"},
	},
	"matrix-coordinate": {
		positive: []string{"// protocol row 4:Sync is the case"},
		negative: []string{"// the address bar row 4 of the table"},
	},
	"review-coordinate": {
		positive: []string{"// widened here (r3)", "// r2 review moved it"},
		negative: []string{"// register r1 holds the count", "// r3c is a cell name"},
	},
	"criterion-number": {
		positive: []string{"// the blind-Close guard, criterion 16", "// criteria 3 covers it"},
		negative: []string{"// the criterion is stated above"},
	},
	"review-must-fix": {
		positive: []string{"// must-fix from the 2026-06-23 review", "// the must-fixes are folded"},
		negative: []string{"// this must fix the ordering", "// a fix is required here"},
	},
	"kb-document-citation": {
		positive: []string{
			"// (KB convention interface-evolution-capability-interfaces)",
			"// treated as attacker-adjacent (KB security-core-hardening",
			"// a published interface is never grown (KB",
		},
		negative: []string{
			"// Create a body larger than maxHistoryBodySize (64 KB).",
			"// a buffer of 64 KB",
			"// KB is 1024 bytes",
		},
	},
	"kb-requirement-number": {
		positive: []string{"// security-core-hardening R4/R7", "// security-core-hardening R4): declared lengths"},
		negative: []string{"// the R4 register", "// read-write access"},
	},
	"document-revision": {
		positive: []string{"// rev 3 put it in the domain"},
		negative: []string{"// reverse the order", "// revision control"},
	},
}

// matchingDetectors returns the names of every detector that fires on line.
func matchingDetectors(line string) []string {
	var out []string
	for _, p := range pointerPatterns {
		if p.re.MatchString(line) {
			out = append(out, p.name)
		}
	}
	sort.Strings(out)
	return out
}

func TestCommentDetectors(t *testing.T) {
	// Both directions: a detector without a fixture is untested, and a fixture
	// naming a detector that no longer exists is cover for nothing.
	declared := map[string]bool{}
	for _, p := range pointerPatterns {
		declared[p.name] = true
	}
	for name := range detectorFixtures {
		if !declared[name] {
			t.Errorf("fixture names %q, which is not a declared detector", name)
		}
	}
	for name := range declared {
		if _, ok := detectorFixtures[name]; !ok {
			t.Errorf("detector %q has no fixture; a detector that is never shown to "+
				"fire may be matching nothing at all", name)
		}
	}

	for name, fx := range detectorFixtures {
		if len(fx.positive) == 0 {
			t.Errorf("detector %q has no positive fixture", name)
		}
		if len(fx.negative) == 0 {
			t.Errorf("detector %q has no negative fixture; sensitivity without "+
				"specificity would pass for a detector that matches everything", name)
		}
		for _, line := range fx.positive {
			got := matchingDetectors(line)
			if len(got) == 0 {
				t.Errorf("%s: %q matched NO detector; this class is invisible to the budget", name, line)
				continue
			}
			found := false
			for _, g := range got {
				if g == name {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: %q was matched by %s but not by %s itself — the class is "+
					"only caught by accident, and would go silent if that other "+
					"detector changed", name, line, strings.Join(got, "+"), name)
			}
		}
		for _, line := range fx.negative {
			if got := matchingDetectors(line); len(got) > 0 {
				t.Errorf("%s: %q is a lookalike and must not count, but matched %s",
					name, line, strings.Join(got, "+"))
			}
		}
	}
}
