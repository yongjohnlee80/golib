package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// modal_answers_test.go: a dialog answers by its buttons' roles — Qt's
// QDialogButtonBox roles — and Enter belongs to the focused button before
// falling back to the dialog's default button.
// The ONE rule, for a native Modal and for the declarative layer over it.

type answersFixture struct {
	h    *harness
	md   *widget.Modal
	host *widget.OverlayHost
	log  []string // loop-owned; read through read()
}

func (f *answersFixture) read() string {
	var s string
	f.h.onLoop(func() { s = strings.Join(f.log, ",") })
	return s
}

// mark presses the Action-role Mark button by its mnemonic and waits until it
// has run — a positive, in-order barrier: every key before it has been handled.
func (f *answersFixture) mark(t *testing.T) {
	t.Helper()
	n := strings.Count(f.read(), "mark")
	f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'm'})
	f.h.waitFor("the mark", func() bool { return strings.Count(f.read(), "mark") > n })
}

func (f *answersFixture) open() bool {
	var o bool
	f.h.onLoop(func() { o = f.md.IsOpen() })
	return o
}

// newAnswers builds Save / Discard / Stay / Mark — the irreversible answer
// FIRST, with no default unless saveIsDefault.
func newAnswers(t *testing.T, saveIsDefault bool, opts ...widget.ModalOption) *answersFixture {
	t.Helper()
	f := &answersFixture{}
	note := func(s string) func() { return func() { f.log = append(f.log, s) } }
	btn := func(label string, key rune, role widget.ButtonRole, extra ...widget.ButtonOption) *widget.Button {
		return widget.NewButton(label, append([]widget.ButtonOption{widget.WithRole(role), widget.WithMnemonic(key),
			widget.WithOnActivate(note(strings.ToLower(label)))}, extra...)...)
	}
	f.md = widget.NewModal(widget.NewText("Unsaved changes"), append([]widget.ModalOption{
		widget.WithButtons(
			btn("Save", 's', widget.ButtonRoleAccept, widget.WithDefault(saveIsDefault)),
			btn("Discard", 'd', widget.ButtonRoleDestructive),
			btn("Stay", 't', widget.ButtonRoleReject),
			btn("Mark", 'm', widget.ButtonRoleAction)),
		widget.WithOnDismiss(func(r widget.DismissReason) { f.log = append(f.log, "dismiss:"+r.String()) }),
	}, opts...)...)
	f.host = widget.NewOverlayHost(widget.NewText(""))
	f.h = startApp(t, f.host, 60, 12)
	f.h.onLoop(func() {
		if err := f.md.Open(f.host); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	f.h.settle()
	return f
}

// Each role's answer, after the button's own callback.
func TestADialogAnswersByItsButtonsRoles(t *testing.T) {
	for _, c := range []struct {
		key  rune
		want string
	}{
		{'s', "save,dismiss:accept"},
		{'d', "discard,dismiss:discard"},
		{'t', "stay,dismiss:cancel"},
		{tui.KeyEscape, "stay,dismiss:cancel"}, // Escape presses the Reject button
	} {
		f := newAnswers(t, false)
		f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: c.key})
		f.h.waitFor("the answer", func() bool { return strings.Contains(f.read(), "dismiss:") })
		if got := f.read(); got != c.want {
			t.Errorf("%q answered %q, want %q", c.key, got, c.want)
		}
		f.h.stop()
	}
}

// An Action-role button answers nothing: its callback, and the dialog stays.
func TestAnActionRoleButtonLeavesTheDialogOpen(t *testing.T) {
	f := newAnswers(t, false)
	defer f.h.stop()
	f.mark(t)
	if got := f.read(); got != "mark" || !f.open() {
		t.Errorf("Mark logged %q, open=%v; want mark alone, still open", got, f.open())
	}
}

// NO DEFAULT: Enter still activates the focused button, by the same path as
// Space. It is not a silent key in a dialog.
func TestEnterPressesTheFocusedButtonWithoutADefault(t *testing.T) {
	for _, c := range []struct {
		name string
		move rune
		want string
	}{
		{"on Save", 0, "save,dismiss:accept"},
		{"on Discard", tui.KeyRight, "discard,dismiss:discard"},
		{"on Discard via Tab", tui.KeyTab, "discard,dismiss:discard"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newAnswers(t, false)
			defer f.h.stop()
			if c.move != 0 {
				f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: c.move})
			}
			f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			f.h.waitFor("the answer", func() bool { return strings.Contains(f.read(), "dismiss:") })
			if got := f.read(); got != c.want {
				t.Errorf("Enter answered %q, want %q", got, c.want)
			}
		})
	}
}

// A DEFAULT: a focused button still gets Enter before the dialog default.
func TestEnterAndSpacePressTheFocusedButtonBeforeTheDefault(t *testing.T) {
	f := newAnswers(t, true)
	f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	f.h.waitFor("the answer", func() bool { return strings.Contains(f.read(), "dismiss:") })
	if got := f.read(); got != "discard,dismiss:discard" {
		t.Errorf("Enter with focus on Discard answered %q, want discard,dismiss:discard", got)
	}
	f.h.stop()

	f = newAnswers(t, true)
	defer f.h.stop()
	f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight}, tui.KeyEvent{Kind: tui.KeyPress, Code: ' '})
	f.h.waitFor("the answer", func() bool { return strings.Contains(f.read(), "dismiss:") })
	if got := f.read(); got != "discard,dismiss:discard" {
		t.Errorf("Space on Discard answered %q, want discard,dismiss:discard", got)
	}
}

// The accept gate: a refused accept leaves the dialog open, the button's own
// callback having run.
func TestAnAcceptGateHoldsTheDialogOpen(t *testing.T) {
	f := newAnswers(t, false, widget.WithAcceptGate(func() bool { return false }))
	defer f.h.stop()
	f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 's'})
	f.mark(t)
	if got := f.read(); got != "save,mark" || !f.open() {
		t.Errorf("a gated accept logged %q, open=%v; want save,mark and still open", got, f.open())
	}
}

// A button taken out of a dialog is a plain button again: its role answers
// nothing, and Enter is its own.
func TestAButtonRemovedFromADialogAnswersNothing(t *testing.T) {
	f := newAnswers(t, false)
	defer f.h.stop()
	var save *widget.Button
	f.h.onLoop(func() {
		save = f.md.Buttons()[0]
		if err := f.md.SetButtons(f.md.Buttons()[1:]...); err != nil {
			t.Error(err)
		}
	})
	f.h.onLoop(func() { save.Activate(tui.OriginProgrammatic) })
	if got := f.read(); got != "save" || !f.open() {
		t.Errorf("a removed Accept button logged %q, open=%v; want save alone, still open", got, f.open())
	}
}

// Only the FINAL, validated list is the dialog's: a button an earlier
// WithButtons named and a later one replaced stays a plain button — its role
// answers nothing and Enter is its own — and a refused list takes no button.
func TestOnlyTheValidatedFinalButtonListIsTheDialogs(t *testing.T) {
	var ran []string
	first := widget.NewButton("First", widget.WithRole(widget.ButtonRoleAccept),
		widget.WithOnActivate(func() { ran = append(ran, "first") }))
	second := widget.NewButton("Second", widget.WithRole(widget.ButtonRoleReject))
	md := widget.NewModal(widget.NewText("body"), widget.WithButtons(first), widget.WithButtons(second))
	if got := md.Buttons(); len(got) != 1 || got[0] != second {
		t.Fatalf("buttons = %v, want the second list", got)
	}
	host := widget.NewOverlayHost(first) // first, standalone, has the keyboard
	h := startApp(t, host, 40, 8)
	defer h.stop()
	h.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Fatal(err)
		}
		first.Activate(tui.OriginProgrammatic)
	})
	var open bool
	h.onLoop(func() { open = md.IsOpen() })
	if strings.Join(ran, ",") != "first" || !open {
		t.Errorf("the superseded button ran %v and left the dialog open=%v; want its callback only, still open", ran, open)
	}

	// A refused list takes no button: the panic comes before any is adopted,
	// so this one, standalone, keeps Enter.
	var lonely []string
	lone := widget.NewButton("Lone", widget.WithRole(widget.ButtonRoleAccept),
		widget.WithOnActivate(func() { lonely = append(lonely, "lone") }))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a list with a nil entry was accepted")
			}
		}()
		widget.NewModal(widget.NewText("body"), widget.WithButtons(lone, nil))
	}()
	h2 := startApp(t, lone, 20, 3)
	defer h2.stop()
	h2.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h2.waitFor("Enter pressed the standalone button", func() bool {
		var n int
		h2.onLoop(func() { n = len(lonely) })
		return n == 1
	})
}
