package terminal

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// THE × AND THE KEYBOARD: a press on a tab's × neither chooses the tab nor leaves the keyboard on
// the tabs themselves (the press's click-to-focus would): the active tab's terminal has it after
// the press, wherever it was before, as after a press on a tab. These cells press through the App,
// so its click-to-focus runs, and type through it.

// typingProc is a resultProc that keeps what is typed into it.
type typingProc struct {
	*resultProc
	mu    sync.Mutex
	typed strings.Builder
}

func (p *typingProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.typed.Write(b)
	return len(b), nil
}

func (p *typingProc) got() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.typed.String()
}

// keyTabs is two started tabs, a (active) and b, each on a typingProc, the × on, and the closes
// asked of the host recorded; the host closes nothing (the "No" of its question).
type keyTabs struct {
	tabs   *TerminalTabs
	a, b   TerminalTabID
	pa, pb *typingProc
	closes []TerminalTabID
}

func newKeyTabs(t *testing.T) *keyTabs {
	t.Helper()
	k := &keyTabs{tabs: NewTerminalTabs(), pa: &typingProc{resultProc: newResultProc()}, pb: &typingProc{resultProc: newResultProc()}}
	t.Cleanup(func() { close(k.pa.release); close(k.pb.release) })
	k.tabs.SetTabCloseButtons(true)
	k.tabs.SetOnTabClose(func(id TerminalTabID) { k.closes = append(k.closes, id) })
	var err error
	if k.a, err = k.tabs.Add("one", WithStarter(func(pty.Cmd) (TerminalProcess, error) { return k.pa, nil })); err != nil {
		t.Fatal(err)
	}
	if k.b, err = k.tabs.Add("two", WithStarter(func(pty.Cmd) (TerminalProcess, error) { return k.pb, nil })); err != nil {
		t.Fatal(err)
	}
	return k
}

// start starts both tabs, a active.
func (k *keyTabs) start(t *testing.T, h *widgettest.Harness) {
	t.Helper()
	h.OnLoop(func() {
		for _, id := range []TerminalTabID{k.a, k.b} {
			if err := k.tabs.Start(id); err != nil {
				t.Error(err)
			}
		}
		if err := k.tabs.Select(k.a); err != nil {
			t.Error(err)
		}
	})
	h.Sync()
}

// pressCross presses tab id's × through the App (its click-to-focus included).
func (k *keyTabs) pressCross(t *testing.T, h *widgettest.Harness, id TerminalTabID) {
	t.Helper()
	var x int
	h.OnLoop(func() {
		for i, tb := range k.tabs.strip.Tabs() {
			if tb.ID == string(id) {
				x = k.tabs.strip.TabSpans()[i].X + 1
			}
		}
	})
	if err := h.TB.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if !widgettest.Wait(h, func() bool { return len(k.closes) > 0 && k.closes[len(k.closes)-1] == id }) {
		t.Fatalf("the press on %s's × asked no close (closes %v)", id, k.closes)
	}
}

// typeTo types s through the App and waits for proc p to be given it.
func typeTo(t *testing.T, h *widgettest.Harness, p *typingProc, s, what string) {
	t.Helper()
	for _, r := range s {
		if err := h.TB.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(p.got(), s) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: %q typed never reached the terminal (it has %q)", what, s, p.got())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The keyboard in the active terminal, a press on the background tab's ×: the close is asked, and
// the next keys reach the active terminal, not the other. The mutant, named here: keepKeys removed
// (the keys stay on the tabs and reach no terminal).
func TestACrossOnABackgroundTabKeepsTheKeysInTheActiveTerminal(t *testing.T) {
	k := newKeyTabs(t)
	h := widgettest.StartApp(t, k.tabs, 40, 6)
	defer h.Stop()
	k.start(t, h)
	k.pressCross(t, h, k.b)
	typeTo(t, h, k.pa, "xy", "after the background tab's ×")
	if got := k.pb.got(); got != "" {
		t.Fatalf("the background tab's terminal was typed into: %q", got)
	}
}

// The active tab's own ×, its host closing nothing (the "No" of its question): the keys are still
// the active terminal's.
func TestACrossOnTheActiveTabTheHostKeepsLeavesTheKeysThere(t *testing.T) {
	k := newKeyTabs(t)
	h := widgettest.StartApp(t, k.tabs, 40, 6)
	defer h.Stop()
	k.start(t, h)
	k.pressCross(t, h, k.a)
	typeTo(t, h, k.pa, "zw", "after the active tab's ×, kept")
}

// The keyboard outside the tabs (a field beside them), a press on a tab's ×: the keyboard goes to
// the active terminal, as a press on a tab takes it there, the user now working in the tabs; the
// field is not typed into.
func TestACrossPressedFromOutsideGivesTheActiveTerminalTheKeys(t *testing.T) {
	k := newKeyTabs(t)
	field := core.NewTextInput()
	root := core.NewSplit(core.Vertical, k.tabs, field)
	h := widgettest.StartApp(t, root, 40, 12)
	defer h.Stop()
	k.start(t, h)
	h.OnLoop(func() { field.Context().RequestFocus() })
	k.pressCross(t, h, k.b)
	typeTo(t, h, k.pa, "qq", "after a × pressed from outside the tabs")
	var v string
	h.OnLoop(func() { v = field.Value() })
	if v != "" {
		t.Fatalf("the field outside the tabs was typed into: %q", v)
	}
}
