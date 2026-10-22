package widget

import (
	"errors"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE EDITOR'S ':' PROMPT, AS A MODULE.
//
// CommandLine is the prompt an editor widget opens on ':': a dialog titled "VIM Commands" holding
// one field. Enter runs the line through the core's RunCommand; Esc closes it; Tab completes the
// verb from the core's Commands and lists the candidates, each with its arguments and help, under
// the field; after the verb, it completes an argument from the command's Complete. It belongs to the widget it opens over (its owner, which must sit under an
// OverlayHost, as an EditorMenu's owner does), so the tui Editor and a graphical one share it.
//
// The prompt closes BEFORE the command runs: a command may open a dialog of its own (the host's
// Save As, its quit question), and closing the prompt after would hand the keyboard back past it.
// A command that fails reopens the prompt with its line and the error under it, to correct.
//
// It also asks replace's questions: at each match a small card, "Replace? y / n / q", over the
// editor with the match selected behind it.
type CommandLine struct {
	on    bool
	owner ContextOwner // the widget it opens over
	core  *EditorCore

	modal *Modal          // the open prompt, nil when closed
	field *TextInput      // its field
	list  *completionList // the dropdown under the field: the candidates, or the error
	comp  completion      // Tab's state over the open prompt

	ask *Modal // replace's open question, nil when none
}

// commandLineWidth is the prompt's width in cells, frame included: room for a candidate's help.
const commandLineWidth = 64

// NewCommandLine is a prompt over owner running lines on core. It is off until SetEnabled(true).
// It opens only while owner is mounted.
func NewCommandLine(owner ContextOwner, core *EditorCore) *CommandLine {
	l := &CommandLine{owner: owner, core: core}
	core.SetCommandLine(func() bool { return l.Open("") })
	core.SetReplaceAsk(l.askReplace)
	return l
}

// SetEnabled turns the prompt on or off; off closes it if it is open, and ':' is then not the
// prompt's (it bubbles).
func (l *CommandLine) SetEnabled(on bool) {
	l.on = on
	if !on {
		l.Close()
	}
}

// Enabled reports whether ':' opens the prompt.
func (l *CommandLine) Enabled() bool { return l.on }

// IsOpen reports whether the prompt is open.
func (l *CommandLine) IsOpen() bool { return l.modal != nil }

// Close closes the prompt, and replace's question with it, if open.
func (l *CommandLine) Close() {
	if l.modal != nil {
		l.modal.Dismiss(DismissCancel)
	}
	if l.ask != nil {
		l.ask.Dismiss(DismissCancel)
	}
}

// host is the overlay host above the owner its prompt dialog opens on; ok false before the owner
// is mounted or with none.
func (l *CommandLine) host() (modalHost, bool) {
	ctx := ownerContext(l.owner)
	if ctx == nil {
		return nil, false
	}
	return hostFor[modalHost](ctx)
}

// Open opens the prompt with initial in its field. It reports false, and ':' bubbles on, when it
// is off, when the core has no commands, or when there is nowhere to open it.
func (l *CommandLine) Open(initial string) bool {
	return l.open(initial, nil)
}

func (l *CommandLine) open(initial string, failed error) bool {
	if !l.on || l.core.Commands() == nil || l.modal != nil {
		return false
	}
	host, ok := l.host()
	if !ok {
		return false
	}
	l.comp = completion{at: -1}
	l.list = &completionList{at: -1}
	if failed != nil {
		l.list.say(promptSays(failed))
	}
	// the field wears the theme's surface, as a field on a card does: a style with no colours
	// would clear its cells to the terminal's own, a hole in the card
	field := style.New().Background(style.TokenSurface).Foreground(style.TokenForeground)
	l.field = NewTextInput(WithInitialValue(initial),
		WithTextInputStyles(TextInputStyles{
			Text:        field,
			Placeholder: mutedPlaceholder().Background(style.TokenSurface),
			Selection:   style.New().Reverse(true),
			Error:       field.Foreground(style.TokenError),
		}),
		WithOnEdit(func(string) { l.comp = completion{at: -1}; l.list.clear() }),
		WithOnSubmit(l.submit))
	body := tui.NewFlex(tui.Vertical)
	body.Add(l.field)
	body.Add(l.list)
	var m *Modal
	m = NewModal(body,
		WithModalTitleMessage(tui.Msg("tui.cmdline.title")),
		WithScrim(false), // a prompt over the page it edits: the page stays as it is, not dimmed
		WithModalWidth(commandLineWidth),
		WithModalRule(true),
		WithModalKeys(l.keys),
		WithModalKeyBinder(func(km *keymap.Map) []keymap.Binding { return tui.BindingsOf(km, cmdlineCommands...) }),
		WithOnDismiss(func(r DismissReason) {
			if l.modal == m {
				l.modal, l.field, l.list = nil, nil, nil
			}
			if r != DismissAccept {
				l.core.CommandLineClosed() // cancelled; an accepted line closes once it has run
			}
		}))
	m.SetFooterMessage(tui.Msg("tui.cmdline.help"))
	if err := m.openOn(host); err != nil {
		return false
	}
	l.modal = m
	return true
}

// submit runs the line, after closing the prompt; a failure reopens it.
func (l *CommandLine) submit(line string) {
	if l.modal != nil {
		l.modal.Dismiss(DismissAccept)
	}
	if err := l.core.RunCommand(line); err != nil && l.open(line, err) {
		return // reopened on the failure: still open
	}
	l.core.CommandLineClosed()
}

// keys are the prompt's own: Tab and Shift+Tab complete; with the dropdown open, Down and Up move
// through it as Tab and Shift+Tab do.
func (l *CommandLine) keys(k tui.KeyEvent) bool {
	if l.field == nil || l.modal == nil {
		return false
	}
	back, ok := cmdlineStep(l.modal.Context(), k, len(l.comp.cands) > 1)
	if !ok {
		return false
	}
	l.complete(back)
	return true
}

// cmdlineStep is the completion step k is, back for the previous candidate. Down and Up step only with
// the dropdown open.
func cmdlineStep(ctx tui.KeyResolver, k tui.KeyEvent, dropdown bool) (back, ok bool) {
	cmd, ok := ctx.KeyCommand(k, cmdlineCommands...)
	if !ok || !dropdown && (cmd.ID == CmdlineNext.ID || cmd.ID == CmdlinePrev.ID) {
		return false, false
	}
	return cmd.ID == CmdlineCompletePrev.ID || cmd.ID == CmdlinePrev.ID, true
}

// cardText is a Text's look on the prompt's card: the card's own colours. A Text with none would
// clear its cells to the screen's default background, a dark block on a themed card.
func cardText() style.Style {
	return style.New().Background(style.TokenPanel).Foreground(style.TokenForeground)
}

// promptSays is what the prompt shows for a command's failure: the command line's own refusals in
// the user's words, never their package name or errs kind; any other error, a host command's, as it
// reads itself.
func promptSays(err error) string {
	var r *commandRefusal
	if errors.As(err, &r) {
		return r.words
	}
	for _, w := range [...]struct {
		err   error
		words string
	}{{ErrNoWriter, noWriterWords}, {ErrNoQuit, noQuitWords}, {ErrReadOnly, readOnlyWords}} {
		if errors.Is(err, w.err) {
			return w.words
		}
	}
	return err.Error()
}
