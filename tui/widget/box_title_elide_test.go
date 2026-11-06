package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// slotText is a title slot the width of the editor's [ Raw | Rendered ] switch.
const slotText = "[ Raw | Rendered ]"

// titleRow is the top border row of a w-wide Box titled title, eliding as e, with a slot of
// slotText when slot is set.
func titleRow(t *testing.T, w int, title string, e widget.Elide, slot bool) string {
	t.Helper()
	opts := []widget.BoxOption{widget.WithTitle(title), widget.WithTitleElide(e)}
	if slot {
		opts = append(opts, widget.WithTitleSlot(widget.NewText(slotText)))
	}
	sh := newShell(widget.NewBox(widget.NewText("body"), opts...))
	h := startApp(t, sh, w, 4)
	defer h.stop()
	return h.row(0)
}

// A long path with its badge after it, on a title row that holds the slot too: elided at the start,
// the badge is whole and the path keeps its tail, "…" where the cut is, the slot's cells untouched.
// (Elided at the end, the badge is what goes.)
func TestATitleElidedAtTheStartKeepsItsBadge(t *testing.T) {
	const title = "/tmp/a/long/project/path/that/runs/on/notes.md  [no workspace]"
	row := titleRow(t, 60, title, widget.ElideLeft, true)
	if !strings.Contains(row, "…") || !strings.Contains(row, "notes.md  [no workspace] ") {
		t.Errorf("the title row %q; want the start cut with … and the tail, badge whole, kept", row)
	}
	if !strings.HasSuffix(row, slotText+"─┐") {
		t.Errorf("the title row %q; want the slot at its right, a border cell after it", row)
	}
	if strings.Contains(row, "/tmp/a") {
		t.Errorf("the title row %q kept the path's start: it should have been cut there", row)
	}
	// the end, as before: the badge is cut off
	if end := titleRow(t, 60, title, widget.ElideRight, true); strings.Contains(end, "[no workspace]") {
		t.Errorf("elided at the end, the title row %q still shows the badge: the cell can't tell the two apart", end)
	}
}

// A wide (CJK) path cut at the start lands on a grapheme boundary: no half of a wide character, the
// "…" where the cut is, and the title still ends in its badge.
func TestATitleElidedAtTheStartNeverSplitsAWideGrapheme(t *testing.T) {
	const title = "文書/資料/調査/報告書.md  [外部]"
	for w := 20; w <= 30; w++ {
		row := titleRow(t, w, title, widget.ElideLeft, false)
		rs := []rune(row)
		if !strings.Contains(row, "[外部] ") || !strings.HasPrefix(row, "┌ …") {
			t.Errorf("width %d: %q; want ┌ … then the title's tail, badge whole", w, row)
		}
		for _, r := range rs {
			if r == '�' {
				t.Errorf("width %d: %q holds a replacement character: a wide grapheme was split", w, row)
			}
		}
		if got := tui.StringWidth(row); got != w {
			t.Errorf("width %d: the title row %q is %d cells wide", w, row, got)
		}
	}
}

// A title that fits is drawn as it is, whatever the elide mode; ElideRight, the default, is the
// box's title as it always was.
func TestATitleThatFitsIsUnchanged(t *testing.T) {
	for _, e := range []widget.Elide{widget.ElideRight, widget.ElideLeft, widget.ElideMiddle, widget.ElidePath} {
		if row := titleRow(t, 60, "notes.md  [no workspace]", e, true); !strings.HasPrefix(row, "┌ notes.md  [no workspace] ─") {
			t.Errorf("elide %d: a title that fits is drawn %q", e, row)
		}
	}
	plain := widget.NewBox(widget.NewText("body"), widget.WithTitle("/tmp/a/long/project/path/notes.md"))
	sh := newShell(plain)
	h := startApp(t, sh, 20, 4)
	defer h.stop()
	if got, want := h.row(0), titleRow(t, 20, "/tmp/a/long/project/path/notes.md", widget.ElideRight, false); got != want {
		t.Errorf("a box with no elide set draws %q; ElideRight draws %q: the default must be the end", got, want)
	}
}

// A frame narrower than the slot and its room drops the slot (never cut, #615's rule), and the title
// then elides into the whole row, at the start.
func TestANarrowFrameDropsTheSlotThenElidesTheTitle(t *testing.T) {
	const w = 2 + len(slotText) + 3 // inner width one short of the slot's + 4
	row := titleRow(t, w, "/tmp/a/long/path/notes.md  [no workspace]", widget.ElideLeft, true)
	if strings.Contains(row, "Raw") {
		t.Errorf("%d columns: %q; want the slot dropped", w, row)
	}
	if !strings.HasPrefix(row, "┌ …") || !strings.Contains(row, "[no workspace]") {
		t.Errorf("%d columns: %q; want the title elided at the start into the whole row, badge whole", w, row)
	}
}
