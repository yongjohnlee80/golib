package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// A Rendered view's pictures sit on the page. A diagram or a chart leaves cells unpainted (around a
// pie's round shape, between two boxes), and those were drawn with no colours at all: blocks of the
// terminal's own background inside a themed page. Each such cell now wears the page's colours, the
// editor's text style. A cell the picture paints keeps its own colours, and the page style's
// attributes (italic here) never reach it.
func TestARenderedPicturesBlankCellsWearThePage(t *testing.T) {
	// the page (the editor's text style) is ANSI 2 and italic; a picture's own cells are drawn on the
	// theme's background token, ANSI 3, so each of the three reads apart
	th := style.NewTheme(style.ANSI(4),
		style.WithToken(style.TokenBackground, style.ANSI(3)),
		style.WithToken(style.TokenForeground, style.ANSI(0)))
	page := style.New().Foreground(style.ANSI(0)).Background(style.ANSI(2)).Italic(true)
	note := "intro\n\n```mermaid\nflowchart LR\n  Alpha --> Beta\n```\n\n" +
		"```mermaid\npie title Pets\n  \"Dogs\" : 80\n  \"Cats\" : 20\n```\nafter\n"
	ed := editor.NewEditor(editor.WithCore(editor.CoreInitialText(note))).WithStyles(core.TextInputStyles{Text: page})
	sh := newShell(ed)
	h := startAppOpts(t, sh, 60, 40, tui.WithTheme(&th))
	defer h.stop()
	h.onLoop(func() {
		ed.SetRenderer(mdRenderer())
		ed.SetView(editor.ViewRendered)
		h.app.FocusInto(ed)
	})
	h.barrier(sh)
	ansi := func(c tui.CellColor, n uint8) bool {
		return (c.Kind == tui.CellColorANSI || c.Kind == tui.CellColorANSI256) && c.Index == n
	}
	var seen string
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("%s\n%s", seen, h.grid())
		}
	})
	h.waitFor("both pictures drawn on the page", func() bool {
		g := h.grid()
		if !strings.Contains(g, "Alpha") || !strings.Contains(g, "Dogs") || strings.Contains(g, "flowchart") {
			seen = "the pictures are not drawn yet"
			return false
		}
		pictureRows := 0
		for y, row := range h.tb.Snapshot() {
			last := -1 // the row's last cell a picture paints: the picture spans the row up to it
			for x, c := range row {
				if ansi(c.Attrs.BG, 3) {
					last = x
				}
			}
			if last < 0 {
				continue
			}
			pictureRows++
			for x := 0; x <= last; x++ {
				c := row[x]
				switch {
				case ansi(c.Attrs.BG, 3) && c.Attrs.Mask&tui.AttrItalic != 0:
					seen = fmt.Sprintf("the picture's cell %q at %d,%d took the page's italic", c.Content, x, y)
					return false
				case !ansi(c.Attrs.BG, 3) && !ansi(c.Attrs.BG, 2):
					seen = fmt.Sprintf("the cell %q at %d,%d inside a picture's row is on %+v, neither the page (ANSI 2) "+
						"nor the picture's own (ANSI 3)", c.Content, x, y, c.Attrs.BG)
					return false
				}
			}
		}
		if pictureRows < 5 {
			seen = fmt.Sprintf("only %d rows hold a picture's cells", pictureRows)
			return false
		}
		return true
	})
}
