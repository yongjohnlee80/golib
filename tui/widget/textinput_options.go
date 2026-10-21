package widget

import "github.com/yongjohnlee80/golib/tui"

// THE TEXTINPUT'S OPTIONS: how a field is configured at construction.

// TextInputOption customizes a TextInput under construction.
type TextInputOption func(*TextInput)

// WithPlaceholder sets the text rendered (muted) while the value is empty.
func WithPlaceholder(s string) TextInputOption {
	return func(t *TextInput) { t.placeholder = plainText(s) }
}

// WithPlaceholderMessage sets the placeholder to a catalog message, shown in the App's
// language.
func WithPlaceholderMessage(m tui.Message) TextInputOption {
	return func(t *TextInput) { t.placeholder = messageText(m) }
}

// WithMask enables password masking: the given rune paints in place of
// every cluster while Value() returns the raw string.
func WithMask(r rune) TextInputOption {
	if r == 0 {
		panic("widget: WithMask: zero mask rune")
	}
	return func(t *TextInput) { t.mask = r }
}

// WithValidate installs the validation hook consulted on Enter.
func WithValidate(fn func(string) error) TextInputOption {
	return func(t *TextInput) { t.validate = fn }
}

// WithOnSubmit installs a SYNCHRONOUS Enter hook, run inside the key handler.
//
// It exists because SubmitEvent is not synchronous, and for one class of
// subscriber that difference is a defect rather than a detail. Bus.Publish
// QUEUES delivery onto the program lane, and the App selects between the input
// lane and the program lane -- so when a keystroke is already waiting, Go
// picks between them pseudo-randomly. A subscriber that moves focus on Enter
// therefore moves it BEFORE or AFTER the next keystroke, about half the time
// each, and the keystroke that loses goes to the input the operator has just
// left.
//
// Measured, in a consumer whose form advances focus on Enter: typing "demo"
// Enter "sqlite" produced a name of "demos" and an engine of "qlite". Every
// paste is that race, since a paste is a burst of keystrokes with none of a
// human's delay.
//
// The SubmitEvent is still published, unchanged, so existing subscribers are
// unaffected. Use this hook only for work that must complete before the next
// key is dispatched -- moving focus, closing the surface -- and the event for
// everything else.
func WithOnSubmit(fn func(string)) TextInputOption {
	return func(t *TextInput) { t.onSubmit = fn }
}

// WithOnEdit calls fn with the value after every EDIT — the moment the widget
// publishes a ChangeEvent, synchronously, inside the key handler. It is not
// called by SetValue: a program replacing the value has made no edit.
func WithOnEdit(fn func(string)) TextInputOption {
	return func(t *TextInput) { t.onEdit = fn }
}

// WithInitialValue sets the starting value (cursor at the end).
func WithInitialValue(s string) TextInputOption {
	return func(t *TextInput) { t.buf.setValue(sanitizeLine(s)) }
}

// WithTextInputStyles overrides the style hooks; zero fields keep defaults.
func WithTextInputStyles(st TextInputStyles) TextInputOption {
	return func(t *TextInput) {
		t.styles = st.over(t.styles)
	}
}
