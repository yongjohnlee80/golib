package markdown_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
)

// The block recognisers are one set for every reader of Markdown (the parser, the highlighter, a
// renderer, an editor's lists): a fence opens with three or more backticks or tildes, up to three
// spaces in, a backtick fence's info holding no backtick; it closes with as many or more of its
// character; a thematic break is three or more of one of '*', '-', '_', with spaces and tabs between;
// frontmatter opens with "---" and closes with "---" or "...".
func TestTheMarkdownBlockRecognisers(t *testing.T) {
	for line, want := range map[string]struct {
		ch byte
		n  int
		ok bool
	}{"```go": {'`', 3, true}, "  ~~~~": {'~', 4, true}, "    ```": {0, 0, false}, "``": {0, 0, false}, "``` a`b": {0, 0, false}} {
		ch, n, ok := markdown.FenceOpens(line)
		if ch != want.ch || n != want.n || ok != want.ok {
			t.Errorf("FenceOpens(%q) = %q, %d, %v; want %q, %d, %v", line, ch, n, ok, want.ch, want.n, want.ok)
		}
	}
	for line, want := range map[string]bool{"```": true, "````  ": true, "``": false, "    ```": false, "``` x": false} {
		if got := markdown.FenceCloses(line, '`', 3); got != want {
			t.Errorf("FenceCloses(%q, `, 3) = %v; want %v", line, got, want)
		}
	}
	for text, want := range map[string]bool{"---": true, "* * *": true, "_\t_ _": true, "--": false, "-*-": false, "--- x": false} {
		if got := markdown.ThematicBreak(text); got != want {
			t.Errorf("ThematicBreak(%q) = %v; want %v", text, got, want)
		}
	}
	if !markdown.FrontmatterOpens("--- ") || markdown.FrontmatterOpens("...") {
		t.Error(`FrontmatterOpens: want "---" only`)
	}
	if !markdown.FrontmatterCloses("...") || !markdown.FrontmatterCloses("---") || markdown.FrontmatterCloses("--") {
		t.Error(`FrontmatterCloses: want "---" or "..."`)
	}
}
