package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A prefix's continuation is its grammar, the literal key: a scheme moving the prefix moves the prefix
// alone. These cells move each prefix golib has and press its literal continuation after it.

func fkey(code rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: code} }

// A table's g moved to F9: F9 then the literal g is the first row; g alone no longer waits.
func TestAMovedTableGoPrefixKeepsItsLiteralSecondG(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.table.goPrefix"><key value="F9"/></shortcut></mapping>`)
	dt := widget.NewDataTable(widget.NewTableCore(numbered(20)))
	sh := newShell(dt)
	h := startAppOpts(t, sh, 40, 12, tui.WithKeymap(m))
	h.inject(tab(), key('G'))
	h.waitFor("G: the last row", func() bool { return tableCursor(h, dt).Row == 19 })
	h.inject(key('g'), key('g'))
	h.barrier(sh)
	if row := tableCursor(h, dt).Row; row != 19 {
		t.Fatalf("g g after the scheme moved the prefix: row %d", row)
	}
	h.inject(fkey(tui.KeyF9), key('g'))
	h.waitFor("F9 g: the first row", func() bool { return tableCursor(h, dt).Row == 0 })
}

// The Terminal's prefix moved to F11: F11 then the literal Ctrl+\ sends one Ctrl+\ to the program,
// and F11 then Ctrl+n is normal mode.
func TestAMovedTerminalPrefixKeepsItsLiteralContinuation(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.terminal.prefix"><key value="F11"/></shortcut></mapping>`)
	f := startTermApp(t, 40, 6, []tui.AppOption{tui.WithKeymap(m)})
	ctrl := func(r rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: r, Mods: tui.ModCtrl} }
	f.h.inject(fkey(tui.KeyF11), ctrl('\\'))
	f.h.barrier(f.sh)
	if n := strings.Count(f.proc().sent(), "\x1c"); n != 1 {
		t.Fatalf("F11 Ctrl+\\ sent %d Ctrl+\\ bytes, want 1: %q", n, f.proc().sent())
	}
	f.h.inject(fkey(tui.KeyF11), ctrl('n'))
	f.h.waitFor("F11 Ctrl+n: normal mode", func() bool { return f.mode() == widget.TerminalNormal })
}

// Normal mode's g moved to F9: F9 then the literal g is the first line.
func TestAMovedTerminalGoPrefixKeepsItsLiteralSecondG(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.terminal.normal.goPrefix"><key value="F9"/></shortcut></mapping>`)
	f := startTermApp(t, 20, 4, []tui.AppOption{tui.WithKeymap(m)}, widget.WithVimKeys(true), widget.WithScrollback(50))
	for i := range 20 {
		f.output(fmt.Sprintf("line %d\r\n", i))
	}
	f.h.inject(key(tui.KeyEscape))
	f.h.waitFor("normal", func() bool { return f.mode() == widget.TerminalNormal })
	row := func() int {
		_, y, _ := f.h.tb.CursorPos()
		return y
	}
	f.h.inject(key('G'))
	f.h.waitFor("G", func() bool { return row() == 3 })
	f.h.inject(fkey(tui.KeyF9), key('g'))
	f.h.waitFor("F9 g: the first line", func() bool { return row() == 0 })
}
