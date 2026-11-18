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
	freed  sync.Once
}

// free lets both processes end when closed (their Close waits on it until then), once.
func (k *keyTabs) free() { k.freed.Do(func() { close(k.pa.release); close(k.pb.release) }) }

func newKeyTabs(t *testing.T) *keyTabs {
	t.Helper()
	k := &keyTabs{tabs: NewTerminalTabs(), pa: &typingProc{resultProc: newResultProc()}, pb: &typingProc{resultProc: newResultProc()}}
	t.Cleanup(k.free)
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

// THE WINDOW BUTTONS AND THE KEYBOARD (SetWindowButtons): a press on _, + or x leaves the keyboard
// in the active terminal before the host acts, as the × does; then the host's act decides.

// pressWindow presses the strip's window control op through the App.
func (k *keyTabs) pressWindow(t *testing.T, h *widgettest.Harness, op core.WindowOperation) {
	t.Helper()
	x := -1
	h.OnLoop(func() {
		for _, c := range k.tabs.strip.TabWindowControls() {
			if c.Op == op {
				x = c.X + c.W/2
			}
		}
	})
	if x < 0 {
		t.Fatalf("no window control %v on the strip", op)
	}
	if err := h.TB.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0}); err != nil {
		t.Fatal(err)
	}
}

// windowTabs is keyTabs beside a field, its window buttons on, and act as the host's answer to a
// window control, run on the loop; it returns the field and the ops pressed.
func windowTabs(t *testing.T, act func(k *keyTabs, op core.WindowOperation)) (*keyTabs, *core.TextInput, *widgettest.Harness, *[]core.WindowOperation) {
	t.Helper()
	k := newKeyTabs(t)
	var ops []core.WindowOperation
	k.tabs.SetWindowButtons(true)
	k.tabs.SetOnWindowButton(func(op core.WindowOperation) {
		ops = append(ops, op)
		if act != nil {
			act(k, op)
		}
	})
	field := core.NewTextInput()
	h := widgettest.StartApp(t, core.NewSplit(core.Vertical, k.tabs, field), 60, 12)
	t.Cleanup(h.Stop)
	k.start(t, h)
	h.OnLoop(func() { field.Context().RequestFocus() })
	return k, field, h, &ops
}

// + (maximize) pressed, the keyboard in the field beside the tabs: the host maximizes (nothing to
// the tree here), and typed keys reach the active terminal, not the field. The mutant, named here:
// keepKeys left off the window buttons' path (the keys stay on the tabs and reach no terminal).
func TestMaximizeOnTheTabsGivesTheActiveTerminalTheKeys(t *testing.T) {
	k, field, h, ops := windowTabs(t, nil)
	k.pressWindow(t, h, core.WindowMaximize)
	if !widgettest.Wait(h, func() bool { return len(*ops) == 1 }) || (*ops)[0] != core.WindowMaximize {
		t.Fatalf("the press asked %v; want maximize", *ops)
	}
	typeTo(t, h, k.pa, "mm", "after + on the tabs")
	var v string
	h.OnLoop(func() { v = field.Value() })
	if v != "" {
		t.Fatalf("the field was typed into after +: %q", v)
	}
}

// _ (minimize) pressed, the host hiding the tabs as a panel's close does: no panic, and the
// keyboard is not left in the hidden tabs: golib's focus repair gives it to the first focusable
// thing left shown, the field here, and typed keys reach it.
func TestMinimizeOnTheTabsTheHostHidingThemLeavesTheKeysOnSomethingShown(t *testing.T) {
	k, field, h, ops := windowTabs(t, func(k *keyTabs, op core.WindowOperation) {
		if op == core.WindowMinimize {
			k.tabs.SetVisible(false)
		}
	})
	k.pressWindow(t, h, core.WindowMinimize)
	if !widgettest.Wait(h, func() bool { return len(*ops) == 1 }) {
		t.Fatal("the press asked nothing")
	}
	h.Sync()
	var inTabs bool
	h.OnLoop(func() { inTabs = k.tabs.Context().FocusWithin(k.tabs) })
	if inTabs {
		t.Fatal("the keyboard was left in the hidden tabs")
	}
	if err := h.TB.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'k', Text: "k"}); err != nil {
		t.Fatal(err)
	}
	if !widgettest.Wait(h, func() bool { return field.Value() == "k" }) {
		t.Fatalf("typed after _ with the tabs hidden: the field has %q; want k", field.Value())
	}
	if k.pa.got() != "" {
		t.Fatalf("a hidden terminal was typed into: %q", k.pa.got())
	}
}

// x (close) pressed, the host removing every tab as a stop of its shells does: no panic, the
// keyboard ends on the emptied tabs (Remove gives them the keyboard their terminal had), which are
// still shown with their empty text, and no terminal is typed into.
func TestCloseOnTheTabsTheHostRemovingThemLeavesTheKeysOnTheEmptyTabs(t *testing.T) {
	k, _, h, ops := windowTabs(t, func(k *keyTabs, op core.WindowOperation) {
		if op == core.WindowClose {
			for _, tb := range k.tabs.Tabs() {
				_, _ = k.tabs.Remove(tb.ID)
			}
		}
	})
	k.free() // the removals' closes return at once
	k.pressWindow(t, h, core.WindowClose)
	if !widgettest.Wait(h, func() bool { return len(*ops) == 1 }) {
		t.Fatal("the press asked nothing")
	}
	if !widgettest.Wait(h, func() bool { return len(k.tabs.Tabs()) == 0 }) {
		t.Fatalf("the host's removal left %d tabs", len(k.tabs.Tabs()))
	}
	var focused bool
	h.OnLoop(func() { focused = k.tabs.Focused() })
	if !focused {
		t.Fatal("after every tab was removed, the emptied tabs don't have the keyboard")
	}
	if a, b := k.pa.got(), k.pb.got(); a != "" || b != "" {
		t.Fatalf("a removed terminal was typed into: %q %q", a, b)
	}
}
