package html_test

import (
	"testing"

	mdhtml "github.com/yongjohnlee80/golib/parse/markdown/html"
)

// ParseSourceSpan reads the source bytes an element shows as SourceSpans writes them, "from-to",
// spaces allowed; anything else, or a span running backwards, is no span.
func TestParseSourceSpanReadsWhatTheWriterWrites(t *testing.T) {
	for v, want := range map[string][3]int{"3-9": {3, 9, 1}, " 3 - 9 ": {3, 9, 1}, "0-0": {0, 0, 1},
		"9-3": {0, 0, 0}, "-1-4": {0, 0, 0}, "3": {0, 0, 0}, "a-b": {0, 0, 0}} {
		from, to, ok := mdhtml.ParseSourceSpan(v)
		if from != want[0] || to != want[1] || ok != (want[2] == 1) {
			t.Errorf("%q: %d, %d, %v; want %d, %d, %v", v, from, to, ok, want[0], want[1], want[2] == 1)
		}
	}
	if mdhtml.SourceSpanAttr != "data-src" {
		t.Errorf("SourceSpanAttr is %q", mdhtml.SourceSpanAttr)
	}
}
