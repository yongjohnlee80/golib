package widget

import (
	"strings"
	"testing"
)

// findAll is a literal find: every match of term, in order.
func findAll(term string) func(string) [][2]int {
	return func(text string) [][2]int {
		var out [][2]int
		for at := 0; ; {
			i := strings.Index(text[at:], term)
			if i < 0 {
				return out
			}
			out = append(out, [2]int{at + i, at + i + len(term)})
			at += i + len(term)
		}
	}
}

// markedRuns is each row's marked text, painted at width: the cells on the warning ground, run by
// run, and whether a run is reversed (the current mark).
func markedRuns(c *cellHTML, width int) []string {
	s := newRecordSurface(width, len(c.rows))
	c.PaintCells(s, 0, width, DocPos{}, DocPos{})
	var out []string
	for y := range s.h {
		var run strings.Builder
		cur := false
		flush := func() {
			if run.Len() > 0 {
				t := run.String()
				if cur {
					t += " (current)"
				}
				out = append(out, t)
				run.Reset()
			}
		}
		for x := range s.w {
			w := styleWords(s.style[y][x])
			if !strings.Contains(w, "bg=warning") {
				flush()
				continue
			}
			if c := strings.Contains(w, "reverse"); c != cur {
				flush()
				cur = c
			}
			run.WriteString(s.text[y][x])
		}
		flush()
	}
	return out
}

// TestTheCellLayoutMarksWhatThePageShows: a find marks the text the page shows. The matches are
// a paragraph's word, a table cell's, and one across two spans (code, then plain). A link's target
// is not shown, so it holds no match. The current mark is drawn reversed, the others not.
func TestTheCellLayoutMarksWhatThePageShows(t *testing.T) {
	c := newCellHTML(func(s string) int { return len([]rune(s)) })
	c.SetSource([]byte(`<p>one kestrel and <a href="kestrel.md">a link</a></p>` +
		`<table><tr><td>kestrel</td><td>x</td></tr></table>` +
		`<p><code>kes</code>trel split</p>`))
	c.SetWidth(40)
	if n := c.SetMarks(findAll("kestrel")); n != 3 {
		t.Fatalf("SetMarks found %d, want 3 (the word, the cell, the one across spans; never the link's target)", n)
	}
	if got := strings.Join(markedRuns(c, 40), "|"); got != "kestrel|kestrel|kestrel" {
		t.Fatalf("marked %q, want each match's text, none current", got)
	}
	c.SetCurrentMark(1)
	if got := strings.Join(markedRuns(c, 40), "|"); got != "kestrel|kestrel (current)|kestrel" {
		t.Fatalf("with the second current: marked %q", got)
	}
	c.SetCurrentMark(3)
	if got := strings.Join(markedRuns(c, 40), "|"); strings.Contains(got, "current") {
		t.Fatalf("a mark out of range left one current: %q", got)
	}
}

// TestACellMarkFollowsItsMatchAcrossAWrap: a match a paragraph wraps is marked on both its rows;
// MarkY is its first row's. A new document is marked again with the find kept, and a find of nil
// clears the marks.
func TestACellMarkFollowsItsMatchAcrossAWrap(t *testing.T) {
	c := newCellHTML(func(s string) int { return len([]rune(s)) })
	c.SetSource([]byte(`<p>first</p><p>aaaa kestrel bbbb</p>`))
	c.SetWidth(8)
	if n := c.SetMarks(findAll("aaaa kestrel")); n != 1 {
		t.Fatalf("SetMarks found %d, want 1", n)
	}
	if got := strings.Join(markedRuns(c, 8), "|"); got != "aaaa|kestrel" {
		t.Fatalf("marked %q, want the match on both its rows", got)
	}
	y, ok := c.MarkY(0)
	if !ok || c.rows[int(y)].block != 1 || !strings.HasPrefix(c.rowText(int(y)), "aaaa") {
		t.Fatalf("MarkY(0) = %v, %v; want the row the match begins on", y, ok)
	}
	c.SetSource([]byte(`<p>aaaa kestrel</p><p>aaaa kestrel</p>`))
	if len(c.m.marks) != 2 {
		t.Fatalf("a new document kept %d marks, want 2, marked with the kept find", len(c.m.marks))
	}
	if c.SetMarks(nil) != 0 || len(markedRuns(c, 8)) != 0 {
		t.Fatal("a nil find left marks")
	}
}

// rowText is row ri's text, as its clusters paint it.
func (c *cellHTML) rowText(ri int) string {
	var sb strings.Builder
	for _, k := range c.rows[ri].cls {
		sb.WriteString(k.text)
	}
	return sb.String()
}
