package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// foldLayout is a fake layout that draws the runs of lines in folds as one, as the GUI's Rendered
// view draws a diagram: closed unless the core is typing (Insert, Replace).
type foldLayout struct {
	fakeLayout
	core  *widget.EditorCore
	folds []foldRun
}

type foldRun struct {
	from, to int
	hidden   bool
}

func (l *foldLayout) FoldOf(line int) (int, int, bool, bool) {
	if m := l.core.Mode(); m == widget.ModeInsert || m == widget.ModeReplace {
		return 0, 0, false, false
	}
	for _, f := range l.folds {
		if line >= f.from && line < f.to {
			return f.from, f.to, f.hidden, true
		}
	}
	return 0, 0, false, false
}

// folded is a core over text whose layout folds folds.
func folded(t *testing.T, text string, folds ...foldRun) *widget.EditorCore {
	t.Helper()
	c := widget.NewEditorCore(widget.CoreInitialText(text))
	c.Bind(nil, &foldLayout{fakeLayout: fakeLayout{per: 1, page: 5}, core: c, folds: folds})
	return c
}

// diagramDoc: a line, a six-line mermaid fence drawn as a picture (lines 1-6), and two lines.
const diagramDoc = "above\n```mermaid\nflowchart LR\n  A --> B\n  B --> C\n  C --> D\n```\nbelow\nend"

var diagram = foldRun{from: 1, to: 7}

func atLine(t *testing.T, c *widget.EditorCore, what string, ln, col int) {
	t.Helper()
	if l, cc := c.Line(); l != ln || cc != col {
		t.Errorf("%s: cursor (%d,%d), want (%d,%d)", what, l, cc, ln, col)
	}
}

// j and k, the arrows, gj and gk count a fold as one row and land on its first line, in its first
// column, keeping the sticky column for the line after; from inside a fold one row leaves it; a
// count counts the fold once.
func TestAFoldIsOneRow(t *testing.T) {
	for _, keys := range []struct{ name, down, up string }{
		{"j and k", "j", "k"}, {"the arrows", "<down>", "<up>"}, {"gj and gk", "gj", "gk"},
	} {
		t.Run(keys.name, func(t *testing.T) {
			c := folded(t, diagramDoc, diagram)
			c.SetLine(0, 2)
			vimSeq(c, keys.down)
			atLine(t, c, "down onto the fold", 1, 0)
			vimSeq(c, keys.down)
			atLine(t, c, "down past it, at the sticky column", 7, 2)
			vimSeq(c, keys.up)
			atLine(t, c, "up onto it", 1, 0)
			vimSeq(c, keys.up)
			atLine(t, c, "up past it", 0, 2)
			c = folded(t, diagramDoc, diagram)
			c.SetLine(4, 0) // inside, as a search or a picture arriving late leaves it
			vimSeq(c, keys.down)
			atLine(t, c, "down from inside", 7, 0)
			c = folded(t, diagramDoc, diagram)
			c.SetLine(4, 0)
			vimSeq(c, keys.up)
			atLine(t, c, "up from inside", 0, 0)
		})
	}
	c := folded(t, diagramDoc, diagram)
	vimSeq(c, "3j")
	atLine(t, c, "3j: the fold, below, end", 8, 0)
	vimSeq(c, "2k")
	atLine(t, c, "2k: below, the fold", 1, 0)
}

// Ctrl-d, Ctrl-u and the page keys count rows: a fold is one.
func TestPagesCountAFoldAsOneRow(t *testing.T) {
	c := folded(t, diagramDoc+"\na\nb\nc\nd", diagram) // half a page of 5 is 2 rows
	vimSeq(c, "<ctd>")
	atLine(t, c, "Ctrl-d: the fold and below", 7, 0)
	vimSeq(c, "<ctu>")
	atLine(t, c, "Ctrl-u: the fold and above", 0, 0)
	vimSeq(c, "<pgdn>")
	atLine(t, c, "PageDown: 5 rows", 10, 0)
}

// A hidden fold is no row: j and k pass it, and k under it at the top goes nowhere; gg can still
// reach it, and j leaves it.
func TestAHiddenFoldIsPassed(t *testing.T) {
	c := folded(t, "---\ntitle: x\n---\nfirst\n---\nmore: y\n---\nlast", foldRun{0, 3, true}, foldRun{4, 7, true})
	c.SetLine(3, 0)
	vimSeq(c, "j")
	atLine(t, c, "j passes the hidden fold", 7, 0)
	vimSeq(c, "k")
	atLine(t, c, "k passes it back", 3, 0)
	vimSeq(c, "k")
	atLine(t, c, "k with only hidden lines above", 3, 0)
	vimSeq(c, "gg")
	atLine(t, c, "gg reaches it", 0, 0)
	vimSeq(c, "j")
	atLine(t, c, "j leaves it", 3, 0)
}

// An operator over a fold takes it whole, linewise: dj and dk across it, dd and x on it, a Visual
// line selection across it; p and P put around it.
func TestAnOperatorTakesAFoldWhole(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ln     int
		keys   string
		result string
	}{
		{"dj from above", 0, "dj", "below\nend"},
		{"dk from below", 7, "dk", "above\nend"},
		{"dd inside", 3, "dd", "above\nbelow\nend"},
		{"x inside", 3, "x", "above\nbelow\nend"},
		{"D inside", 1, "D", "above\nbelow\nend"},
		{"Vj d", 0, "Vjd", "below\nend"},
		{"vj d (charwise, made whole)", 0, "vjd", "below\nend"},
		{"p after it", 8, "yy2kp", "above\n```mermaid\nflowchart LR\n  A --> B\n  B --> C\n  C --> D\n```\nend\nbelow\nend"},
		{"P before it, from inside", 8, "yy4GP", "above\nend\n```mermaid\nflowchart LR\n  A --> B\n  B --> C\n  C --> D\n```\nbelow\nend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := folded(t, diagramDoc, diagram)
			c.SetLine(tc.ln, 0)
			vimSeq(c, tc.keys)
			if got := c.Value(); got != tc.result {
				t.Errorf("%s:\n%s\nwant\n%s", tc.keys, got, tc.result)
			}
		})
	}
}

// Typing opens the folds: in Insert the arrows walk the block's lines, and what is typed goes where
// the cursor is; Esc closes them again.
func TestTypingOpensAFold(t *testing.T) {
	c := folded(t, diagramDoc, diagram)
	c.SetLine(1, 0)
	vimSeq(c, "i<down>X")
	atLine(t, c, "Insert walks into the block", 2, 1)
	vimSeq(c, "<esc>j")
	atLine(t, c, "Normal again: j leaves the fold", 7, 0)
	if !strings.Contains(c.Value(), "\nXflowchart LR\n") {
		t.Errorf("the typed text:\n%s", c.Value())
	}
}

// Without folds, nothing changes: j and k are lines, dj is two lines.
func TestNoFoldsNoChange(t *testing.T) {
	c := folded(t, diagramDoc)
	vimSeq(c, "jj")
	atLine(t, c, "jj", 2, 0)
	vimSeq(c, "dj")
	if got := c.Value(); got != "above\n```mermaid\n  B --> C\n  C --> D\n```\nbelow\nend" {
		t.Errorf("dj: %q", got)
	}
}
