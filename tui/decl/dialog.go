package decl

import (
	"errors"
	"fmt"

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
//	close() from a handler                   closed only — nobody answered
//
// Its look is the card golib draws — the title in the frame, the message, a
// rule, the buttons, and an optional `helpText` under them — coloured through
// the palette roles like every other widget.

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

	accepted         func(args ...qml.SpecValue)
	rejected, closed func()
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
	return nil
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
	align       widget.ButtonAlign
	// buttons are laid out in this order.
	buttons []standardButton
	hooks   dialogHooks
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
	opts := []widget.ModalOption{
		widget.WithModalTitle(s.title),
		widget.WithButtons(buttons...),
		widget.WithModalRule(len(buttons) > 0),
		widget.WithScrim(s.dim),
		widget.WithButtonAlign(s.align),
		widget.WithOnDismiss(d.dismissed),
	}
	if s.help != "" {
		opts = append(opts, widget.WithModalFooter(s.help))
	}
	d.modal = widget.NewModal(s.body, opts...)
	return d
}

func buildDialog(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, fmt.Errorf("Dialog needs exactly 1 child, its content, got %d (at %s)",
			len(b.Children), b.Pos)
	}
	s := dialogSpec{body: b.Children[0], dim: true, align: widget.ButtonsCenter}
	var flags int64
	consumed, err := readProps(b.Props, map[string]field{
		"title":           into(&s.title, stringOf),
		"helpText":        into(&s.help, stringOf),
		"dim":             into(&s.dim, boolOf),
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
	return newDialog(b, s), consumed, nil
}
