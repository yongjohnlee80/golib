package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
)

// theme_test.go holds the theme claim to the screen: the layout names no
// colour, the import line picks the theme, and the colours that reach the
// cells are that theme's.

const retroImport = "import editor.theme.retro 1.0"

// withImport is editor.qml with its theme import line replaced — the ONLY edit
// switching theme is supposed to need.
func withImport(t *testing.T, line string) []byte {
	t.Helper()
	if !bytes.Contains(layout, []byte(retroImport)) {
		t.Fatalf("editor.qml no longer imports %q; update this test", retroImport)
	}
	return bytes.Replace(layout, []byte(retroImport), []byte(line), 1)
}

func ansi(n uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorANSI, Index: n} }

func rgb(r, g, b uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: b} }

// The CGA colours retro is written in.
var (
	cgaBlack  = rgb(0, 0, 0)
	cgaBlue   = rgb(0, 0, 0xaa)
	cgaGreen  = rgb(0, 0xaa, 0)
	cgaCyan   = rgb(0, 0xaa, 0xaa)
	cgaRed    = rgb(0xaa, 0, 0)
	cgaGrey   = rgb(0xaa, 0xaa, 0xaa)
	cgaYellow = rgb(0xff, 0xff, 0x55)
	cgaWhite  = rgb(0xff, 0xff, 0xff)
)

var terminalDefault = tui.CellColor{}

// cell is one screen cell's colours and attributes.
func (r *running) cell(t *testing.T, x, y int) tui.CellAttrs {
	t.Helper()
	grid := r.be.Snapshot()
	if y >= len(grid) || x >= len(grid[y]) {
		t.Fatalf("cell (%d,%d) is off the screen", x, y)
	}
	return grid[y][x].Attrs
}

// labelAt finds a label's first COLUMN on a row — counted in runes, since a
// box-drawing border is one column and three bytes. Every glyph on this screen
// is one column wide.
func (r *running) labelAt(t *testing.T, row int, label string) int {
	t.Helper()
	line := r.rows()[row]
	i := strings.Index(line, label)
	if i < 0 {
		t.Fatalf("%q is not on row %d:\n%s", label, row, r.screen())
	}
	return utf8.RuneCountInString(line[:i])
}

type look struct {
	what   string
	x, y   int
	fg, bg tui.CellColor
}

func (r *running) expect(t *testing.T, looks []look) {
	t.Helper()
	for _, l := range looks {
		got := r.cell(t, l.x, l.y)
		if got.FG != l.fg || got.BG != l.bg {
			t.Errorf("%s at (%d,%d): fg %+v bg %+v, want fg %+v bg %+v",
				l.what, l.x, l.y, got.FG, got.BG, l.fg, l.bg)
		}
		// REVERSE is an attribute, not a swap: a reversed cell reports the
		// colours it was given and shows the opposite pair.
		if got.Mask&tui.AttrReverse != 0 {
			t.Errorf("%s at (%d,%d) is reversed, so it shows fg %+v on bg %+v",
				l.what, l.x, l.y, got.BG, got.FG)
		}
	}
}

// TestRetroIsTheShippedTheme: black on grey chrome, red access keys, and the
// document on dark blue — CGA colours, not palette slots.
func TestRetroIsTheShippedTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := start(t, path)
	f := r.labelAt(t, 0, "File")
	ty := rowOf(r.rows(), "hello")
	tx := r.labelAt(t, ty, "hello")
	last := len(r.rows()) - 1
	for last > 0 && strings.TrimSpace(r.rows()[last]) == "" {
		last--
	}
	r.expect(t, []look{
		{"the File access key", f, 0, cgaRed, cgaGrey},
		{"the rest of File", f + 1, 0, cgaBlack, cgaGrey},
		{"the document's text", tx, ty, cgaYellow, cgaBlue},
		{"the frame border", tx - 1, ty, cgaWhite, cgaBlue}, // focused: the editor has the keyboard
		{"the status line", 0, last, cgaBlack, cgaGrey},
	})
	// Past the text: the fill, which carries only a background.
	if got := r.cell(t, tx+20, ty+2).BG; got != cgaBlue {
		t.Errorf("the empty document area: bg %+v, want dark blue", got)
	}
	if r.cell(t, f, 0).Mask&tui.AttrUnderline == 0 {
		t.Error("the access key lost its underline")
	}
}

// TestSwitchingToMonoIsTheImportLineAlone: the same layout, one line changed.
func TestSwitchingToMonoIsTheImportLineAlone(t *testing.T) {
	r := startLayout(t, "", withImport(t, "import editor.theme.mono 1.0"))
	f := r.labelAt(t, 0, "File")
	last := len(r.rows()) - 1
	for last > 0 && strings.TrimSpace(r.rows()[last]) == "" {
		last--
	}
	r.expect(t, []look{
		{"the File access key", f, 0, ansi(0), ansi(7)},
		{"the rest of File", f + 1, 0, ansi(0), ansi(7)},
		{"the document area", 10, 5, terminalDefault, terminalDefault},
		{"the status line", 0, last, ansi(0), ansi(7)},
	})
	if r.cell(t, f, 0).Mask&tui.AttrUnderline == 0 {
		t.Error("mono's access key is not underlined, so nothing marks it")
	}
}

// TestImportingBothThemesIsRefused: two modules exporting Theme is an
// ambiguity, and it is reported rather than resolved by whichever came last.
func TestImportingBothThemesIsRefused(t *testing.T) {
	src := withImport(t, retroImport+"\nimport editor.theme.mono 1.0")
	_, _, err := New(Options{Schedule: func(func()) {}, Layout: src})
	if !errors.Is(err, decl.ErrDuplicateExport) {
		t.Fatalf("err = %v, want ErrDuplicateExport", err)
	}
}

// TestTheLayoutNamesNoColour: the claim the theme split rests on, checked on
// the file rather than taken on trust.
func TestTheLayoutNamesNoColour(t *testing.T) {
	src := string(layout)
	for _, f := range []string{"dialogs/QuitDialog.qml", "dialogs/AboutDialog.qml"} {
		b, err := dialogFiles.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src += "\n" + string(b)
	}
	for _, line := range strings.Split(src, "\n") {
		code, _, _ := strings.Cut(line, "//")
		if !strings.Contains(code, "palette.") {
			continue
		}
		if _, value, ok := strings.Cut(code, ":"); !ok || !strings.HasPrefix(strings.TrimSpace(value), "Theme.") {
			t.Errorf("a palette role is not bound to the theme: %q", strings.TrimSpace(line))
		}
	}
}

// TestRetroHighlightsTheSelectedRowInGreen: the dropdown's selected row, with
// its access key still red on it — the hotkey look merged over the row's.
func TestRetroHighlightsTheSelectedRowInGreen(t *testing.T) {
	r := start(t, "")
	r.key(t, alt('f'))
	r.waitFor(t, "the File dropdown", func(s string) bool { return strings.Contains(s, "Save") })
	y := rowOf(r.rows(), "New")
	x := r.labelAt(t, y, "New")
	s := r.labelAt(t, rowOf(r.rows(), "Save"), "Save")
	r.expect(t, []look{
		{"the selected row's access key", x, y, cgaRed, cgaGreen},
		{"the selected row's text", x + 1, y, cgaBlack, cgaGreen},
		{"an unselected row", s + 1, rowOf(r.rows(), "Save"), cgaBlack, cgaGrey},
	})
}

// TestRetroSelectsTextInCyan: the editor's visual selection takes the theme's
// highlight roles, unreversed, and the text past it keeps the document's.
func TestRetroSelectsTextInCyan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := start(t, path)
	r.key(t, runeKey('v'), runeKey('l'))
	r.waitFor(t, "visual mode", func(s string) bool { return strings.Contains(s, "VISUAL") })
	y := rowOf(r.rows(), "hello")
	x := r.labelAt(t, y, "hello")
	r.expect(t, []look{
		{"a selected letter", x + 1, y, cgaBlack, cgaCyan},
		{"an unselected letter", x + 3, y, cgaYellow, cgaBlue},
	})
}
