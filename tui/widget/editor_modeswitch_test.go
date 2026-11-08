package widget_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// updateSwitch rewrites the switch's theme goldens (testdata/modeswitch).
var updateSwitch = flag.Bool("update-modeswitch", false, "rewrite the mode switch's theme goldens (testdata/modeswitch)")

// switchEditor is a terminal Editor holding a CSV text, its table its document view, turned on,
// the switch shown, focused, at 40×10, under th (nil: the default theme), its text in page (as a
// host's palette gives it: AutoDoc's Frame, palette.text on palette.base; zero: the default).
func switchEditor(t *testing.T, th *style.Theme, page ...style.Style) (*harness, *shell, *widget.Editor) {
	t.Helper()
	opts := []widget.EditorOption{widget.WithCore(widget.CoreInitialText("name,n\nann,1\nbob,2\n")), widget.EditorModeSwitch(true)}
	if len(page) > 0 {
		opts = append(opts, widget.WithEditorStyles(widget.TextInputStyles{Text: page[0]}))
	}
	ed := widget.NewEditor(opts...)
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	sh := newShell(ed)
	var appOpts []tui.AppOption
	if th != nil {
		appOpts = append(appOpts, tui.WithTheme(th))
	}
	h := startAppOpts(t, sh, 40, 10, appOpts...)
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	return h, sh, ed
}

// switchAt is the column of the switch's first cell in a 40-column editor.
const switchAt = 40 - widget.ModeSwitchWidth

func pressAt(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y}
}

func chosen(h *harness, ed *widget.Editor) (v widget.EditorView) {
	h.onLoop(func() { v = ed.View() })
	return
}

// The switch sits on the editor's first row, at its right, the text under it: the text's first line
// on the second row, the cursor there, and a press on the text lands where it is drawn. A press on
// a segment chooses its view; Ctrl+T and the host's SetView move the switch's mark the same way.
func TestTheTerminalEditorsModeSwitch(t *testing.T) {
	h, sh, ed := switchEditor(t, nil)
	defer h.stop()
	if r := h.row(0); r != strings.Repeat(" ", switchAt)+"[ Raw | Rendered ]" {
		t.Fatalf("row 0: %q; want the switch at its right", r)
	}
	if !strings.HasPrefix(h.row(1), "name,n") {
		t.Fatalf("row 1: %q; want the text's first line under the switch", h.row(1))
	}
	var cx, cy int
	var ok bool
	h.onLoop(func() { cx, cy, ok = ed.Cursor() })
	if !ok || cx != 0 || cy != 1 {
		t.Errorf("the cursor at %d,%d (%v); want 0,1: the text's first line, under the switch", cx, cy, ok)
	}
	h.inject(pressAt(2, 3)) // "bob,2": its third cluster
	h.barrier(sh)
	var ln, col int
	h.onLoop(func() { ln, col = ed.Core().Line() })
	if ln != 2 || col != 2 {
		t.Errorf("a press on row 3, column 2 put the cursor at %d:%d; want 2:2", ln, col)
	}
	h.inject(pressAt(switchAt+len(widget.ModeSwitchRaw)+3, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRendered || !strings.HasPrefix(strings.TrimSpace(h.row(1)), "name") {
		t.Fatalf("a press on Rendered: %v, row 1 %q; want the table under the switch", chosen(h, ed), h.row(1))
	}
	if r := h.row(0); !strings.HasSuffix(r, "[ Raw | Rendered ]") {
		t.Errorf("the switch over the table: %q", r)
	}
	h.inject(pressAt(switchAt+2, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw {
		t.Fatalf("a press on Raw: %v", chosen(h, ed))
	}
	// the mark follows the view, whoever chose it
	mark := func() string {
		cells := h.tb.Snapshot()[0]
		raw := cells[switchAt+2].Attrs.Mask&tui.AttrBold != 0
		ren := cells[switchAt+len(widget.ModeSwitchRaw)+3].Attrs.Mask&tui.AttrBold != 0
		return fmt.Sprintf("raw=%v rendered=%v", raw, ren)
	}
	if m := mark(); m != "raw=true rendered=false" {
		t.Errorf("Raw chosen: %s", m)
	}
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if m := mark(); chosen(h, ed) != widget.ViewRendered || m != "raw=false rendered=true" {
		t.Errorf("after Ctrl+T: %v, %s", chosen(h, ed), m)
	}
	h.onLoop(func() { ed.SetView(widget.ViewRaw) })
	h.barrier(sh)
	if m := mark(); m != "raw=true rendered=false" {
		t.Errorf("after the host's SetView(Raw): %s", m)
	}
}

// Rendered unavailable for the document shown: the segment is faint, and a press on it changes
// nothing, as Ctrl+T then does nothing.
func TestTheModeSwitchsFaintRendered(t *testing.T) {
	h, sh, ed := switchEditor(t, nil)
	defer h.stop()
	h.onLoop(func() { ed.SetRenderedDocument(false) })
	h.barrier(sh)
	cells := h.tb.Snapshot()[0]
	if c := cells[switchAt+len(widget.ModeSwitchRaw)+3]; c.Attrs.Mask&tui.AttrFaint == 0 {
		t.Errorf("Rendered unavailable is not faint: %+v", c.Attrs)
	}
	if c := cells[switchAt+2]; c.Attrs.Mask&tui.AttrFaint != 0 {
		t.Errorf("Raw is faint: %+v", c.Attrs)
	}
	h.inject(pressAt(switchAt+len(widget.ModeSwitchRaw)+3, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw || !strings.HasPrefix(h.row(1), "name,n") {
		t.Errorf("a press on a faint Rendered: %v, row 1 %q; want Raw kept", chosen(h, ed), h.row(1))
	}
}

// With no document view there is nothing to switch to: no row is given up, the text starts at the
// top. The switch is the host's to turn on: off, a document view gives no row up either.
func TestTheModeSwitchTakesNoRowWithoutADocumentView(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("first\nsecond")), widget.EditorModeSwitch(true))
	sh := newShell(ed)
	h := startApp(t, sh, 40, 4)
	defer h.stop()
	h.barrier(sh)
	if !strings.HasPrefix(h.row(0), "first") || strings.Contains(h.grid(), "Rendered") {
		t.Errorf("no document view: row 0 %q, grid\n%s", h.row(0), h.grid())
	}
	off := widget.NewEditor(widget.WithCore(widget.CoreInitialText("first\nsecond")))
	off.SetDocumentView(widget.NewTableDocument(off.Core(), ','))
	sh2 := newShell(off)
	h2 := startApp(t, sh2, 40, 4)
	defer h2.stop()
	h2.barrier(sh2)
	if !strings.HasPrefix(h2.row(0), "first") || strings.Contains(h2.grid(), "Rendered") {
		t.Errorf("the switch off: row 0 %q", h2.row(0))
	}
}

// switchPages are the page's text on its page in each theme, as a host's palette gives the editor.
var switchPages = map[string]style.Style{
	"sepia": style.New().Foreground(style.RGB(0x5b, 0x46, 0x36)).Background(style.RGB(0xe6, 0xd9, 0xb9)),
	"dark":  style.New().Foreground(style.RGB(0xd0, 0xd0, 0xd0)).Background(style.RGB(0x1c, 0x1c, 0x1c)),
}

// switchThemes are a sepia page and a dark one, as AutoDoc's themes set their text and page.
var switchThemes = map[string]style.Theme{
	"sepia": style.NewTheme(style.RGB(0x8a, 0x73, 0x50),
		style.WithToken(style.TokenBackground, style.RGB(0xe6, 0xd9, 0xb9)),
		style.WithToken(style.TokenForeground, style.RGB(0x5b, 0x46, 0x36))),
	"dark": style.NewTheme(style.RGB(0x5f, 0x87, 0xd7),
		style.WithToken(style.TokenBackground, style.RGB(0x1c, 0x1c, 0x1c)),
		style.WithToken(style.TokenForeground, style.RGB(0xd0, 0xd0, 0xd0))),
}

// cellLook is a cell's colours and its bold, underline and faint, as a golden spells them.
func cellLook(c tui.Cell) string {
	col := func(cc tui.CellColor) string {
		switch cc.Kind {
		case tui.CellColorRGB:
			return fmt.Sprintf("%02x%02x%02x", cc.R, cc.G, cc.B)
		case tui.CellColorDefault:
			return "default"
		}
		return fmt.Sprintf("kind%d:%d", cc.Kind, cc.Index)
	}
	var a string
	for _, f := range []struct {
		bit  tui.AttrMask
		name string
	}{{tui.AttrBold, "bold"}, {tui.AttrUnderline, "underline"}, {tui.AttrFaint, "faint"}} {
		if c.Attrs.Mask&f.bit != 0 {
			a += " " + f.name
		}
	}
	return "fg " + col(c.Attrs.FG) + " bg " + col(c.Attrs.BG) + a
}

// The switch reads in a sepia page and a dark one: its text wears the editor's own text colour on
// the editor's page, the chosen segment bold and underlined, an unavailable Rendered faint over the
// same colours, never a colour of its own. A golden per theme of each segment's look in each state.
func TestTheModeSwitchReadsInEveryTheme(t *testing.T) {
	for name, th := range switchThemes {
		t.Run(name, func(t *testing.T) {
			h, sh, ed := switchEditor(t, &th, switchPages[name])
			defer h.stop()
			var b strings.Builder
			look := func(state string) {
				cells := h.tb.Snapshot()[0]
				fmt.Fprintf(&b, "%s\n  Raw      %s\n  |        %s\n  Rendered %s\n  text     %s\n", state,
					cellLook(cells[switchAt+2]), cellLook(cells[switchAt+len(widget.ModeSwitchRaw)]),
					cellLook(cells[switchAt+len(widget.ModeSwitchRaw)+3]), cellLook(h.tb.Snapshot()[1][0]))
			}
			look("Raw chosen")
			h.inject(ctrlEv('t'))
			h.barrier(sh)
			look("Rendered chosen")
			h.onLoop(func() { ed.SetView(widget.ViewRaw); ed.SetRenderedDocument(false) })
			h.barrier(sh)
			look("Rendered unavailable")
			got := b.String()
			path := filepath.Join("testdata", "modeswitch", name+".golden")
			if *updateSwitch {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: %v (-update-modeswitch)", path, err)
			}
			if got != string(want) {
				t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
			}
			// the segments wear the text's colours: what the text reads in, they read in
			cells := h.tb.Snapshot()
			if text, sw := cells[1][0].Attrs, cells[0][switchAt+2].Attrs; text.FG != sw.FG || text.BG != sw.BG {
				t.Errorf("the switch's colours %+v differ from the text's %+v", sw, text)
			}
			// and they are the page's: its text colour on its page, the contrast the theme chose
			if fg, bg := cells[0][switchAt+2].Attrs.FG, cells[0][switchAt+2].Attrs.BG; fg.Kind != tui.CellColorRGB || bg.Kind != tui.CellColorRGB {
				t.Errorf("the switch is not in the page's colours: fg %+v bg %+v", fg, bg)
			}
		})
	}
}

// The wheel on the switch's row scrolls what is under it, as anywhere: the text in Raw, the
// document view (here the table) when it is shown. A reader scrolling with the pointer near the top
// does not stall there.
func TestTheWheelOnTheSwitchsRowScrollsWhatIsUnderIt(t *testing.T) {
	var csv strings.Builder
	csv.WriteString("name,n\n")
	for i := range 40 {
		fmt.Fprintf(&csv, "row%02d,%d\n", i, i)
	}
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText(csv.String())), widget.EditorModeSwitch(true))
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	sh := newShell(ed)
	h := startApp(t, sh, 40, 10)
	defer h.stop()
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	wheel := tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 3, Y: 0}

	before := h.row(1)
	h.inject(wheel, wheel)
	h.barrier(sh)
	if after := h.row(1); after == before || !strings.HasSuffix(h.row(0), "[ Raw | Rendered ]") {
		t.Errorf("Raw: the wheel on the switch's row left the text at %q (was %q)", after, before)
	}

	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRendered {
		t.Fatal("Ctrl+T did not show the table")
	}
	before = h.grid()
	h.inject(wheel, wheel, wheel)
	h.barrier(sh)
	if after := h.grid(); after == before || !strings.HasSuffix(h.row(0), "[ Raw | Rendered ]") {
		t.Errorf("Rendered: the wheel on the switch's row did not scroll the table:\n%s", after)
	}
}

// A press on the switch reaches it with the focus elsewhere: the page shown takes the editor's
// focus as it arrives, so a press that focused the editor first went nowhere. The switch is resolved
// before focus moves, as a toolbar's control is, and leaves the focus where it was.
func TestTheSwitchTakesAPressWithTheFocusElsewhere(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("name,n\nann,1\n")), widget.EditorModeSwitch(true))
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	ed.SetView(widget.ViewRendered)
	other := widget.NewTextInput()
	sh := newShell(widget.NewSplit(widget.Horizontal, other, ed))
	h := startApp(t, sh, 80, 8)
	defer h.stop()
	h.onLoop(func() { h.app.FocusInto(other) })
	h.barrier(sh)
	x, y := -1, -1
	for row := range 8 {
		if i := strings.Index(h.row(row), "[ Raw | Rendered ]"); i >= 0 {
			x, y = len([]rune(h.row(row)[:i])), row
			break
		}
	}
	if y < 0 {
		t.Fatalf("no switch:\n%s", h.grid())
	}
	h.inject(pressAt(x+2, y))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw {
		t.Fatalf("a press on Raw with the focus in another widget: %v; want Raw", chosen(h, ed))
	}
	var stays bool
	h.onLoop(func() { stays = h.app.FocusWithin(other) })
	if !stays {
		t.Error("the press on the switch moved the focus")
	}
	h.inject(pressAt(x+len(widget.ModeSwitchRaw)+3, y))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRendered {
		t.Errorf("a press on Rendered: %v", chosen(h, ed))
	}
}

// An editor narrower than the switch shows as much of it as fits, from its left, and a press is hit
// where it is drawn: on the visible Raw segment it chooses Raw.
func TestANarrowEditorsSwitchIsHitWhereItIsDrawn(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("a,b\n1,2\n")), widget.EditorModeSwitch(true))
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	ed.SetView(widget.ViewRendered)
	sh := newShell(ed)
	const w = 12
	if w >= widget.ModeSwitchWidth {
		t.Fatalf("precondition: %d is not narrower than the switch (%d)", w, widget.ModeSwitchWidth)
	}
	h := startApp(t, sh, w, 6)
	defer h.stop()
	h.barrier(sh)
	if r := h.row(0); !strings.HasPrefix(r, "[ Raw |") {
		t.Fatalf("row 0 of a %d-column editor: %q; want the switch from its left", w, r)
	}
	h.inject(pressAt(2, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw {
		t.Errorf("a press on the visible Raw: %v; want Raw", chosen(h, ed))
	}
}
