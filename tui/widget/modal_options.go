package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// MODAL OPTIONS: how a dialog is built (its buttons, title, footer, size, placement, scrim, keys)
// and the button row's alignment.

// ModalOption configures a Modal at construction.
type ModalOption func(*Modal)

// WithButtons supplies the dialog's buttons. The list is validated at
// construction; see [NewModal] for what is refused.
func WithButtons(b ...*Button) ModalOption {
	return func(m *Modal) { m.card.buttons = append([]*Button(nil), b...) }
}

// WithAcceptGate says whether an Accept answer may accept NOW: a file dialog
// with nothing chosen yet. A refused accept leaves the dialog open. The button's
// own callback has run either way.
func WithAcceptGate(fn func() bool) ModalOption {
	return func(m *Modal) { m.acceptGate = fn }
}

// WithOnDismiss sets the callback run when the dialog closes. It runs before
// the dismissal event reaches the bus, so an observer sees settled state.
func WithOnDismiss(fn func(DismissReason)) ModalOption {
	return func(m *Modal) { m.onDismiss = fn }
}

// WithModalTitle sets the card's title text.
//
// Named WithModalTitle rather than WithTitle because the package already has a
// WithTitle for Box; this one is qualified, and ModalStyle.WithTitle — a method
// — sets the title's LOOK rather than its text.
func WithModalTitle(s string) ModalOption {
	return func(m *Modal) { m.card.title = plainText(s) }
}

// WithModalTitleMessage sets the card's title to a catalog message, shown in the App's
// language.
func WithModalTitleMessage(msg tui.Message) ModalOption {
	return func(m *Modal) { m.card.title = messageText(msg) }
}

// WithModalRule draws a line across the card between the body and the buttons,
// joined to the frame at both ends: the message above it, the decision below.
// Without it the two are separated by a blank line.
func WithModalRule(v bool) ModalOption {
	return func(m *Modal) { m.card.rule = v }
}

// WithModalKeys gives the dialog keys of its own: fn sees every key press the
// controls inside it leave unconsumed, while it is open, and reports whether it
// took it — a viewer's `y` copying, say.
func WithModalKeys(fn func(tui.KeyEvent) bool) ModalOption {
	return func(m *Modal) { m.ownKeys = fn }
}

// WithModalWidth sets the card's width in cells, frame included, where it
// would otherwise take its content's: a prompt whose field would fill the
// screen. Clamped to the host; 0 is the content's width.
func WithModalWidth(w int) ModalOption {
	return func(m *Modal) { m.card.width = max(w, 0) }
}

// WithModalMaxSizePercent caps the card's available width and height as a
// percentage of the host. Zero on either axis leaves that axis uncapped.
// The card remains centered and can still shrink with the terminal.
func WithModalMaxSizePercent(width, height int) ModalOption {
	return func(m *Modal) {
		m.maxWidthPercent, m.maxHeightPercent = clampPct(width), clampPct(height)
	}
}

// WithModalFooter sets a line of help text under the buttons — the keys the
// dialog answers to, say. It belongs to the CARD, not the body, so the buttons
// stay the card's and keep their mnemonics: a body cannot resolve a bare
// letter, which is what moving the buttons into one to get a footer below
// them would cost.
func WithModalFooter(text string) ModalOption {
	return func(m *Modal) { m.card.footer = plainText(text) }
}

// WithModalStyle associates a style. The Modal does not own it; several dialogs
// may share one.
func WithModalStyle(s *ModalStyle) ModalOption {
	return func(m *Modal) { m.card.st = s }
}

// ButtonAlign is where a dialog's row of buttons sits within its card.
//
// The zero value CENTRES them, which is the conventional look for a
// confirmation and the one a caller who says nothing should get: a pair of
// controls hugging one edge of a card wider than they are reads as detached
// from the question above.
type ButtonAlign uint8

const (
	// ButtonsCenter centres the row. The default.
	ButtonsCenter ButtonAlign = iota
	// ButtonsLeft packs the row against the leading edge.
	ButtonsLeft
	// ButtonsRight packs it against the trailing edge, the placement a form-like
	// dialog with a wide body usually wants.
	ButtonsRight
)

// Valid reports whether a is one of the declared alignments.
func (a ButtonAlign) Valid() bool { return a <= ButtonsRight }

// String names the alignment for traces and test failures.
func (a ButtonAlign) String() string {
	switch a {
	case ButtonsCenter:
		return "center"
	case ButtonsLeft:
		return "left"
	case ButtonsRight:
		return "right"
	}
	return "unknown"
}

// WithModalVimNavigation adds h/k and l/j as aliases for the arrow keys that
// move between a dialog's buttons. Off by default.
//
// OPT-IN, because h and l are ordinary letters: a dialog whose buttons carry
// mnemonics may legitimately answer to one of them, and a consumer that is not
// a Vim-shaped application should not have two of its letters quietly taken.
// An explicit mnemonic still wins over an alias, so enabling this cannot make
// a declared key unreachable.
func WithModalVimNavigation(v bool) ModalOption {
	return func(m *Modal) { m.vimKeys = v }
}

// WithModalDismissKeys adds plain keys that close the dialog, alongside Escape.
//
// A configured key is an ESCAPE EQUIVALENT in full: it resolves through the same
// path, so a dialog with a Cancel-role button has that button ACTIVATED, and the
// dismissal is reported as DismissEscape — the reason names the intention, not
// the key. A key that closed the dialog without running Cancel would be a
// second, quieter way out with different consequences.
//
// A terminal application usually has a second dismiss key — `q` is the common
// one — and a dialog that traps focus swallows it, so a surface that honoured it
// as a Float stops honouring it as a Modal with nothing to say so. The keys
// belong to the HOST rather than to this widget: only the application knows
// which of its dialogs a reader navigates and which take typed input, and a `q`
// that closes the dialog is a `q` the operator cannot type into it.
//
// RESOLVED AFTER MNEMONICS AND AFTER THE FOCUSED CONTROL. A declared mnemonic is
// specific intent and wins; and a dismiss key never reaches this while a control
// that consumes text has focus, because the runtime offers the key there first.
// Both orderings matter: a dialog whose Cancel button is mnemonic `q` keeps
// activating Cancel, and a dialog containing a text input keeps typing `q`.
//
// Modified keys are never dismiss keys — Ctrl-Q is not `q`.
func WithModalDismissKeys(keys ...rune) ModalOption {
	return func(m *Modal) {
		m.dismissKeys = append(m.dismissKeys, keys...)
	}
}

// WithButtonAlign sets where the button row sits. An invalid value is refused
// at construction rather than stored, like every other closed set here.
func WithButtonAlign(a ButtonAlign) ModalOption {
	return func(m *Modal) {
		if !a.Valid() {
			panic(tuiFatal("widget: WithButtonAlign",
				"value outside the declared ButtonAlign set", int(a)))
		}
		m.card.align = a
	}
}

// WithPlacement sets where the card sits within the host. Default is centred,
// which is what a dialog almost always wants — and what a plain stack layer
// does NOT do on its own, since a stack places an unaligned layer top-left.
//
// An invalid value is refused at construction rather than stored.
func WithPlacement(p ModalPlacement) ModalOption {
	return func(m *Modal) {
		if !p.Valid() {
			panic(tuiFatal("widget: WithPlacement",
				"value outside the declared ModalPlacement set", int(p)))
		}
		m.placement = p
	}
}

// WithScrim sets whether the background is dimmed behind this dialog. Default
// is true. This decides WHETHER; ModalStyle.WithScrim decides what it looks
// like.
func WithScrim(v bool) ModalOption {
	return func(m *Modal) { m.wantScrim = v }
}

// WithModalKeyBinder says what a WithModalKeys handler binds, so App.KeyBindings lists it after the
// Modal's own commands, in the order the Modal consults them.
func WithModalKeyBinder(fn func(*keymap.Map) []keymap.Binding) ModalOption {
	return func(m *Modal) { m.keyBinder = fn }
}
