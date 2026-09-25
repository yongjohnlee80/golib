package decl

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// DIALOGS — Qt Quick Controls' Dialog, over golib's Modal.
//
//	Dialog {
//	    id: quitDialog
//	    title: "Quit"
//	    standardButtons: Dialog.Yes | Dialog.No
//	    onAccepted: App.quit()
//	    Text { text: "Are you sure to quit?" }
//	}
//	MenuItem { text: "E&xit"; onTriggered: quitDialog.open() }
//
// THE DIALOG OWNS ITS WHOLE LIFECYCLE. It opens when a handler calls open(),
// and it closes itself: on its buttons, on their mnemonics, and on Escape. A
// document writes what an ANSWER does — onAccepted, onRejected — and nothing
// about how the dialog goes away, because that is the same in every dialog and
// a consumer writing it is a consumer who can get it wrong.
//
// Every way out resolves to one of Qt's two answers, in one place (dismissed):
//
//	an accepting button (Ok, Save, Yes)      accepted, then closed
//	a rejecting button, or Escape            rejected, then closed
//	a DestructiveRole button (Discard)       closed only — nobody answered
//	close() from a handler                   closed only — nobody answered
//
// Opening raises `opened`, as Qt's Popup does: a prompt clearing its field.
//
// Its look is the card golib draws — the title in the frame, the message, a
// rule, the buttons, and an optional `helpText` under them — coloured through
// the palette roles like every other widget. With no buttons, the rule
// separates the message from the help line. It is as wide as its content,
// unless `width` — Qt's Popup.width, in cells — says otherwise: a dialog
// holding a TextField, which fills whatever it is given.

// standardButton is one of Qt's standard buttons: its flag, its label with the
// mnemonic Qt gives it, and whether choosing it accepts.
type standardButton struct {
	name   string
	bit    int64
	label  string
	accept bool
}

// dialogStandardButtons are Qt's, with Qt's flag values, in the order they are
// laid out: the affirmative first, as the question is read.
var dialogStandardButtons = []standardButton{
	{"Ok", 0x00000400, "&OK", true},
	{"Save", 0x00000800, "&Save", true},
	{"Yes", 0x00004000, "&Yes", true},
	{"No", 0x00010000, "&No", false},
	{"Cancel", 0x00400000, "&Cancel", false},
	{"Close", 0x00200000, "C&lose", false},
}

// dialogButtons is the flag set a document combines: `Dialog.Yes | Dialog.No`.
var dialogButtons = func() flagSet {
	f := flagSet{singleton: "Dialog", values: map[string]int64{}}
	for _, b := range dialogStandardButtons {
		f.values[b.name] = b.bit
	}
	return f
}()

// dialogNode is the component a Dialog or FileDialog declaration builds. It
// takes no place in the layout: its Window gives it the overlay host, and it
// opens there.
type dialogNode struct {
	modal *widget.Modal
	host  *widget.OverlayHost
	// afterClose is the Window's, run once the dialog has gone: the keyboard
	// goes back to where the document says it lives.
	afterClose func()
	hooks      dialogHooks
	// chooser is a FileDialog's body, nil for a Dialog; selected is the file it
	// starts from, as its selectedFile says.
	chooser  widget.FileChooser
	selected string
	// highlighters are the adapter's definitions: a FileDialog's preview
	// highlights a file by its name with them.
	highlighters *highlight.Repository

	accepted                 func(args ...qml.SpecValue)
	opened, rejected, closed func()
}

// dialogHooks are what a KIND of dialog adds to the one lifecycle every dialog
// shares. Each is optional; a plain Dialog uses none.
type dialogHooks struct {
	// gate says whether an accepting button may accept NOW. A file dialog's
	// Open on a folder opens the folder instead, and stays.
	gate func() bool
	// opened runs once the dialog is up — to put the keyboard in the right
	// place inside it, say.
	opened func()
	// acceptArgs are the parameters `accepted` is raised with.
	acceptArgs func() []qml.SpecValue
}

func (*dialogNode) Init(*tui.Context)               {}
func (*dialogNode) Layout(tui.Constraints) tui.Size { return tui.Size{} }
func (*dialogNode) Render(tui.Surface)              {}
func (*dialogNode) HandleEvent(tui.Event) bool      { return false }

var errDialogOutsideWindow = errors.New("a Dialog opens over a Window, and this one is not in one; " +
	"inside a Go program, give the adapter WithOverlay(host)")

// SetOverlay implements [Overlaid]: the Window's host, and what the Window
// runs once the dialog has closed.
func (d *dialogNode) SetOverlay(host *widget.OverlayHost, afterClose func()) {
	d.host, d.afterClose = host, afterClose
}

var _ Overlaid = (*dialogNode)(nil)

// releaseDialog closes a dialog whose node is going away — a reload dropped
// it while it was open. Its modal lives on the overlay host, not in the node,
// so forgetting the node alone left it on screen, trapping the keyboard, with
// no id left to close it by. Closed programmatically: the document removed
// the question, and nobody answered it.
func releaseDialog(c tui.Component) {
	if d, ok := c.(*dialogNode); ok && d.modal.IsOpen() {
		d.modal.Dismiss(widget.DismissProgrammatic)
	}
}

// open shows the dialog. Opening an open dialog is not an error: a menu row
// pressed twice asked the same question twice.
func (d *dialogNode) open() error {
	if d.host == nil {
		return errDialogOutsideWindow
	}
	if d.modal.IsOpen() {
		return nil
	}
	if err := d.modal.Open(d.host); err != nil {
		return err
	}
	if d.hooks.opened != nil {
		d.hooks.opened()
	}
	d.opened()
	return nil
}

// setTitle is Dialog.title's setter.
func (d *dialogNode) setTitle(s string) { d.modal.SetTitle(s) }

// setHelp is Dialog.helpText's setter: the help line under the body, with the
// rule that separates them when the dialog has no buttons to.
func (d *dialogNode) setHelp(s string) {
	d.modal.SetFooter(s)
	if len(d.modal.Buttons()) == 0 {
		d.modal.SetRule(s != "")
	}
}

// close hides the dialog without answering it.
func (d *dialogNode) close() error {
	d.modal.Dismiss(widget.DismissProgrammatic)
	return nil
}

// accept closes the dialog with the affirmative answer.
func (d *dialogNode) accept() { d.modal.Dismiss(widget.DismissAccept) }

// dismissed is the ONE place a way out becomes an answer. Buttons dismiss with
// their answer, Escape arrives here on its own, and close() with neither.
func (d *dialogNode) dismissed(reason widget.DismissReason) {
	switch reason {
	case widget.DismissAccept:
		var args []qml.SpecValue
		if d.hooks.acceptArgs != nil {
			args = d.hooks.acceptArgs()
		}
		d.accepted(args...)
	case widget.DismissCancel, widget.DismissEscape:
		d.rejected()
	}
	d.closed()
	if d.afterClose != nil {
		d.afterClose()
	}
}

// dialogSpec is everything one dialog is built from, whatever kind it is.
type dialogSpec struct {
	body        tui.Component
	title, help string
	dim         bool
	width       int
	align       widget.ButtonAlign
	// buttons are laid out in this order.
	buttons []standardButton
	// shortcuts are the dialog's own keys, live while it is open.
	shortcuts []*shortcutNode
	// box is its DialogButtonBox, nil for standardButtons.
	box   *buttonBoxNode
	hooks dialogHooks
}

// newDialog is the ONE construction of a dialog: its buttons and what each
// does, its card, and its lifecycle. Dialog and FileDialog differ only in the
// spec they hand it, so a way out cannot behave differently in one of them.
func newDialog(b Build, s dialogSpec) *dialogNode {
	d := &dialogNode{
		// A Window hands its dialogs its own host when it arranges them;
		// until then — and outside any Window — the adapter's, if it has one.
		host:     b.Overlay,
		hooks:    s.hooks,
		accepted: b.EmitterWith("accepted"),
		opened:   b.Emitter("opened"),
		rejected: b.Emitter("rejected"),
		closed:   b.Emitter("closed"),
	}
	buttons := make([]*widget.Button, 0, len(s.buttons))
	for _, sb := range s.buttons {
		label, key, _ := mnemonic(sb.label)
		role, press := widget.ButtonRoleCancel, func() { d.modal.Dismiss(widget.DismissCancel) }
		if sb.accept {
			role, press = widget.ButtonRoleDefault, func() {
				if d.hooks.gate == nil || d.hooks.gate() {
					d.accept()
				}
			}
		}
		opts := []widget.ButtonOption{
			widget.WithRole(role),
			widget.WithMnemonic(key),
			widget.WithOnActivate(press),
		}
		buttons = append(buttons, widget.NewButton(label, opts...))
	}
	if s.box != nil {
		buttons = append(buttons, s.box.answer(d)...)
	}
	opts := []widget.ModalOption{
		widget.WithModalTitle(s.title),
		widget.WithButtons(buttons...),
		widget.WithModalRule(len(buttons) > 0 || s.help != ""),
		widget.WithModalWidth(s.width),
		widget.WithScrim(s.dim),
		widget.WithButtonAlign(s.align),
		widget.WithOnDismiss(d.dismissed),
	}
	if s.help != "" {
		opts = append(opts, widget.WithModalFooter(s.help))
	}
	if s.box != nil {
		// No default: a box's answers may be irreversible, so no key answers
		// for the user before they have chosen a button.
		opts = append(opts, widget.WithModalNoImplicitAnswer())
	}
	if len(s.shortcuts) > 0 {
		shortcuts := s.shortcuts
		opts = append(opts, widget.WithModalKeys(func(k tui.KeyEvent) bool {
			for _, sc := range shortcuts {
				if sc.seq.matches(k) {
					sc.trigger()
					return true
				}
			}
			return false
		}))
	}
	d.modal = widget.NewModal(s.body, opts...)
	return d
}

func buildDialog(b Build) (tui.Component, []string, error) {
	body, shortcuts, box, err := dialogChildren(b)
	if err != nil {
		return nil, nil, err
	}
	s := dialogSpec{body: body, dim: true, align: widget.ButtonsCenter, shortcuts: shortcuts, box: box}
	var flags int64
	consumed, err := readProps(b.Props, map[string]field{
		"title":           into(&s.title, stringOf),
		"helpText":        into(&s.help, stringOf),
		"dim":             into(&s.dim, boolOf),
		"width":           into(&s.width, cellsOf),
		"standardButtons": into(&flags, dialogButtons.read),
	})
	if err != nil {
		return nil, nil, err
	}
	for _, sb := range dialogStandardButtons {
		if flags&sb.bit != 0 {
			s.buttons = append(s.buttons, sb)
		}
	}
	if s.box != nil && len(s.buttons) > 0 {
		return nil, nil, fmt.Errorf("a Dialog answers with standardButtons or a DialogButtonBox, not both (at %s)", b.Pos)
	}
	return newDialog(b, s), consumed, nil
}

// cellsOf reads a size in cells: a whole number, not negative.
func cellsOf(v qml.SpecValue) (int, error) {
	n, err := numberOf(v)
	if err != nil {
		return 0, err
	}
	if n < 0 || n != float64(int(n)) {
		return 0, fmt.Errorf("want a whole number of cells, got %s (at %s)", v.Raw, v.Pos)
	}
	return int(n), nil
}

// dialogChildren takes a Dialog's children: exactly one CONTENT item, and any
// number of Shortcuts — Qt's non-visual children, here keys that are live while
// the dialog is open and the controls in it leave them.
func dialogChildren(b Build) (tui.Component, []*shortcutNode, *buttonBoxNode, error) {
	var body tui.Component
	var shortcuts []*shortcutNode
	var box *buttonBoxNode
	n := 0
	for _, c := range b.Children {
		if sc, ok := c.(*shortcutNode); ok {
			shortcuts = append(shortcuts, sc)
			continue
		}
		if bb, ok := c.(*buttonBoxNode); ok {
			if box != nil {
				return nil, nil, nil, fmt.Errorf("a Dialog holds one DialogButtonBox (at %s)", b.Pos)
			}
			box = bb
			continue
		}
		body = c
		n++
	}
	if n != 1 {
		return nil, nil, nil, fmt.Errorf("Dialog needs exactly 1 child, its content, got %d, besides its Shortcuts "+
			"and DialogButtonBox (at %s)", n, b.Pos)
	}
	return body, shortcuts, box, nil
}
