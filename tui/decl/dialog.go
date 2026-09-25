package decl

import (
	"errors"
	"fmt"

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
	f := flagSet{singleton: "Dialog", prop: "standardButtons", values: map[string]int64{}}
	for _, b := range dialogStandardButtons {
		f.values[b.name] = b.bit
	}
	return f
}()

// dialogNode is the component a Dialog declaration builds. It takes no place in
// the layout: its Window gives it the overlay host, and it opens there.
type dialogNode struct {
	modal *widget.Modal
	host  *widget.OverlayHost
	// afterClose is the Window's, run once the dialog has gone: the keyboard
	// goes back to where the document says it lives.
	afterClose func()

	accepted, rejected, closed func()
}

func (*dialogNode) Init(*tui.Context)               {}
func (*dialogNode) Layout(tui.Constraints) tui.Size { return tui.Size{} }
func (*dialogNode) Render(tui.Surface)              {}
func (*dialogNode) HandleEvent(tui.Event) bool      { return false }

var errDialogOutsideWindow = errors.New("a Dialog opens over a Window, and this one is not in one")

// open shows the dialog. Opening an open dialog is not an error: a menu row
// pressed twice asked the same question twice.
func (d *dialogNode) open() error {
	if d.host == nil {
		return errDialogOutsideWindow
	}
	if d.modal.IsOpen() {
		return nil
	}
	return d.modal.Open(d.host)
}

// close hides the dialog without answering it.
func (d *dialogNode) close() error {
	d.modal.Dismiss(widget.DismissProgrammatic)
	return nil
}

// dismissed is the ONE place a way out becomes an answer. Buttons dismiss with
// their answer, Escape arrives here on its own, and close() with neither.
func (d *dialogNode) dismissed(reason widget.DismissReason) {
	switch reason {
	case widget.DismissAccept:
		d.accepted()
	case widget.DismissCancel, widget.DismissEscape:
		d.rejected()
	}
	d.closed()
	if d.afterClose != nil {
		d.afterClose()
	}
}

func buildDialog(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, fmt.Errorf("Dialog needs exactly 1 child, its content, got %d (at %s)",
			len(b.Children), b.Pos)
	}
	var title, help string
	var flags int64
	dim := true
	p := palette{}
	consumed, err := readProps(b.Props, withPalette(map[string]field{
		"title":           into(&title, stringOf),
		"helpText":        into(&help, stringOf),
		"dim":             into(&dim, boolOf),
		"standardButtons": into(&flags, dialogButtons.read),
	}, p, dialogRoles))
	if err != nil {
		return nil, nil, err
	}
	d := &dialogNode{
		accepted: b.Emitter("accepted"),
		rejected: b.Emitter("rejected"),
		closed:   b.Emitter("closed"),
	}
	cardStyle, buttonStyle := p.dialogStyles()
	var buttons []*widget.Button
	for _, sb := range dialogStandardButtons {
		if flags&sb.bit == 0 {
			continue
		}
		label, key, _ := mnemonic(sb.label)
		role, reason := widget.ButtonRoleCancel, widget.DismissCancel
		if sb.accept {
			role, reason = widget.ButtonRoleDefault, widget.DismissAccept
		}
		opts := []widget.ButtonOption{
			widget.WithRole(role),
			widget.WithMnemonic(key),
			widget.WithOnActivate(func() { d.modal.Dismiss(reason) }),
		}
		if buttonStyle != nil {
			opts = append(opts, widget.WithButtonStyle(buttonStyle))
		}
		buttons = append(buttons, widget.NewButton(label, opts...))
	}
	opts := []widget.ModalOption{
		widget.WithModalTitle(title),
		widget.WithButtons(buttons...),
		widget.WithModalRule(len(buttons) > 0),
		widget.WithScrim(dim),
		widget.WithOnDismiss(d.dismissed),
	}
	if help != "" {
		opts = append(opts, widget.WithModalFooter(help))
	}
	if cardStyle != nil {
		opts = append(opts, widget.WithModalStyle(cardStyle))
	}
	d.modal = widget.NewModal(b.Children[0], opts...)
	return d, consumed, nil
}
