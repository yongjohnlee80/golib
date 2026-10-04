package yaml

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// Every prefix of a valid stream is, by definition, a stream that more text completes: the rest of
// it. Incomplete is reported only where the parser can tell positively that the input ran out (the
// grammar wanted more and got the end of the stream, the scanner ran out inside a construct, a
// character was cut off), so it is certain where it is reported but does not catch every
// truncation. This measures the share it catches over every failing proper prefix (cut at rune
// boundaries) of every valid yaml-test-suite input, and holds it from falling: a change that made
// the rule report fewer truncations would show here. That no malformed stream is reported Incomplete
// is pinned case by case in TestError_IsAParseSyntaxError.
func TestIncomplete_FailingPrefixesOfValidStreams(t *testing.T) {
	t.Parallel()
	dirs, _ := filepath.Glob("testdata/yaml-test-suite/*")
	sub, _ := filepath.Glob("testdata/yaml-test-suite/*/*")
	dirs = append(dirs, sub...)
	streams, failing, incomplete := 0, 0, 0
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, "error")); err == nil {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, "in.yaml"))
		if err != nil || !utf8.Valid(src) {
			continue
		}
		if _, err := Parse(src); err != nil {
			continue
		}
		streams++
		for cut := 0; cut < len(src); cut++ {
			if !utf8.RuneStart(src[cut]) {
				continue
			}
			_, err := Parse(src[:cut])
			if err == nil {
				continue
			}
			failing++
			var ye *Error
			if !errors.As(err, &ye) {
				t.Errorf("%s: prefix %d: %v is not an *Error", dir, cut, err)
				continue
			}
			if ye.Incomplete {
				incomplete++
			}
		}
	}
	if streams < 200 || failing == 0 {
		t.Fatalf("the instrument saw %d streams and %d failing prefixes; it did not run", streams, failing)
	}
	share := float64(incomplete) / float64(failing)
	t.Logf("%d streams, %d failing prefixes, %d reported Incomplete (%.1f%%)", streams, failing, incomplete, 100*share)
	if share < minIncompleteShare {
		t.Errorf("only %.1f%% of truncations reported Incomplete, below the %.1f%% measured when the rule was written", 100*share, 100*minIncompleteShare)
	}
}

// minIncompleteShare is the share of truncations reported Incomplete when the rule was written:
// 5368 of 5608 failing prefixes of the 308 valid suite streams (95.7%), with every Incomplete site
// identified by its own check that the input ran out. The suite is fixed test data, so the share is
// exact and repeatable; a rule change that loses any of them fails here.
const minIncompleteShare = 5368.0 / 5608.0
