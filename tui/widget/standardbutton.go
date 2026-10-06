package widget

import (
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
)

// StandardButton is one of a dialog's conventional buttons, as Qt's
// QDialogButtonBox::StandardButton names them: its label comes from golib's catalogs, so it
// reads in the App's language, and its role says what choosing it means.
type StandardButton int

const (
	// StandardOk is "OK": it accepts.
	StandardOk StandardButton = iota
	// StandardSave is "Save": it accepts.
	StandardSave
	// StandardYes is "Yes": it accepts.
	StandardYes
	// StandardNo is "No": it rejects.
	StandardNo
	// StandardCancel is "Cancel": it rejects.
	StandardCancel
	// StandardClose is "Close": it rejects.
	StandardClose
)

// standardButtons is each standard button's catalog id and role, by StandardButton.
var standardButtons = [...]struct {
	id   string
	role ButtonRole
}{
	StandardOk:     {"tui.button.ok", ButtonRoleAccept},
	StandardSave:   {"tui.button.save", ButtonRoleAccept},
	StandardYes:    {"tui.button.yes", ButtonRoleAccept},
	StandardNo:     {"tui.button.no", ButtonRoleReject},
	StandardCancel: {"tui.button.cancel", ButtonRoleReject},
	StandardClose:  {"tui.button.close", ButtonRoleReject},
}

// Message is the catalog message the button is labelled with: "tui.button.yes" for
// [StandardYes].
func (b StandardButton) Message() tui.Message { return tui.Msg(standardButtons[b.index()].id) }

// Role is what choosing the button means: [ButtonRoleAccept] for Ok, Save and Yes,
// [ButtonRoleReject] for the rest.
func (b StandardButton) Role() ButtonRole { return standardButtons[b.index()].role }

func (b StandardButton) index() int {
	if b < 0 || int(b) >= len(standardButtons) {
		panic(errs.Fatal{Op: "widget: StandardButton", Rule: "not one of the declared standard buttons",
			Detail: itoa(max(int(b), 0))})
	}
	return int(b)
}

// NewStandardButton builds b with its role and its catalog label: the label and its
// mnemonic follow the App's language, and the mnemonic stays on English's letter in every
// one ("&Yes" is "예(&y)" in Korean). opts apply after, so a caller may still make it the
// default or disable it.
func NewStandardButton(b StandardButton, opts ...ButtonOption) *Button {
	all := append([]ButtonOption{WithRole(b.Role()), WithLabelMessage(b.Message())}, opts...)
	return NewButton("", all...)
}
