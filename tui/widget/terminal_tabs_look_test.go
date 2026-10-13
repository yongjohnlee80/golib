package widget

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/style"
)

// terminal_tabs_look_test.go: a tab renamed (SetLabel), the body with no tab (SetEmptyText), and
// the look every tab takes (SetMinimumContrast, SetCursorColor) or its own (WithMinimumContrast,
// WithCursorColor).

func TestTerminalTabsRenameATabAndSayWhatTheEmptyBodyIs(t *testing.T) {
	tabs := NewTerminalTabs()
	changes := 0
	tabs.SetOnChanged(func() { changes++ })
	p := newResultProc()
	defer close(p.release)
	id := addTestTerminal(t, tabs, "one", p)
	before := changes
	if err := tabs.SetLabel(id, "codename-one"); err != nil {
		t.Fatal(err)
	}
	if got := tabs.Tabs()[0].Label; got != "codename-one" || changes != before+1 {
		t.Errorf("label %q, %d changes; want the new label, one change", got, changes-before)
	}
	if err := tabs.SetLabel(id, "codename-one"); err != nil || changes != before+1 {
		t.Errorf("the same label again: %v, %d changes", err, changes-before)
	}
	if err := tabs.SetLabel("terminal-none", "x"); !errors.Is(err, ErrTerminalTabUnknown) {
		t.Errorf("an unknown tab: %v", err)
	}

	empty := NewTerminalTabs()
	h := startAppInternal(t, empty, 40, 4)
	defer h.stopInternal()
	h.syncInternal()
	if got := h.tb.String(); !strings.Contains(got, "No terminals. Use + to add one.") {
		t.Fatalf("the default empty body:\n%s", got)
	}
	h.onLoopInternal(func() { empty.SetEmptyText("No agent running") })
	h.syncInternal()
	if got := h.tb.String(); !strings.Contains(got, "No agent running") || strings.Contains(got, "No terminals") {
		t.Errorf("the empty body after SetEmptyText:\n%s", got)
	}
}

func TestTerminalTabsGiveEveryTabTheirLookAndATabItsOwn(t *testing.T) {
	red, blue := style.RGB(255, 0, 0), style.RGB(0, 0, 255)
	tabs := NewTerminalTabs()
	p1, p2, p3 := newResultProc(), newResultProc(), newResultProc()
	defer close(p1.release)
	defer close(p2.release)
	defer close(p3.release)
	own := addTestTerminalWith(t, tabs, "own", p1, WithMinimumContrast(7), WithCursorColor(blue))
	if term := pickTerminal(tabs, own); term.minContrast != 7 {
		t.Errorf("a tab's own contrast: %v", term.minContrast)
	} else if c, ok := term.CursorColor(); !ok || c != blue {
		t.Errorf("a tab's own cursor colour: %v %v", c, ok)
	}
	plain := addTestTerminal(t, tabs, "plain", p2)
	if term := pickTerminal(tabs, plain); term.minContrast != 0 {
		t.Errorf("a tab with no look took one: %v", term.minContrast)
	}
	// the container's look reaches every tab, those already added and those added later
	tabs.SetMinimumContrast(4.5)
	tabs.SetCursorColor(red)
	later := addTestTerminal(t, tabs, "later", p3)
	for _, id := range []TerminalTabID{own, plain, later} {
		term := pickTerminal(tabs, id)
		c, ok := term.CursorColor()
		if term.minContrast != 4.5 || !ok || c != red {
			t.Errorf("%s: contrast %v, cursor %v %v; want the container's", id, term.minContrast, c, ok)
		}
	}
}

func addTestTerminalWith(t *testing.T, tabs *TerminalTabs, label string, p *resultProc, opts ...TerminalOption) TerminalTabID {
	t.Helper()
	id, err := tabs.Add(label, append(opts, WithStarter(func(pty.Cmd) (TerminalProcess, error) { return p, nil }))...)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
