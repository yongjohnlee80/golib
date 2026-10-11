package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// THE EDITOR'S ':' PROMPT, AS A MODULE.
//
// CommandLine is the prompt an editor widget opens on ':': a dialog titled "VIM Commands" holding
// one field. Enter runs the line through the core's RunCommand; Esc closes it; Tab completes the
// verb from the core's Commands and lists the candidates, each with its arguments and help, under
// the field. It belongs to the widget it opens over (its owner, which must sit under an
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
	owner tui.Component // the widget it opens over
	core  *EditorCore

	modal *Modal     // the open prompt, nil when closed
	field *TextInput // its field
	list  *Text      // the candidates or the error, under the field
	comp  completion // Tab's state over the open prompt

	ask *Modal // replace's open question, nil when none
}

// completion is Tab's walk through the candidates: what was typed when it began, and which
// candidate the last Tab chose (-1: only the common prefix so far).
type completion struct {
	typed string
	cands []Command
	at    int
}

// commandLineTitle is the prompt's title; commandLineHelp its help line.
const (
	commandLineTitle = "VIM Commands"
	commandLineHelp  = "Enter runs · Tab completes · Esc closes"
	commandLineWidth = 64
)

// NewCommandLine is a prompt over owner running lines on core. It is off until SetEnabled(true).
// owner must have a Context (a mounted widget) when the prompt opens.
func NewCommandLine(owner tui.Component, core *EditorCore) *CommandLine {
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

// host is the OverlayHost above the owner; ok false before the owner is mounted or with none.
func (l *CommandLine) host() (*OverlayHost, bool) {
	ider, ok := l.owner.(interface{ Context() *tui.Context })
	if !ok || ider.Context() == nil {
		return nil, false
	}
	return hostFor[*OverlayHost](ider.Context())
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
	l.list = NewText("", WithWrapMode(Wrap))
	if failed != nil {
		// the package's sentinels name it for a log; the user reads only what went wrong
		l.list.SetText(strings.TrimPrefix(failed.Error(), "widget: "))
	}
	l.field = NewTextInput(WithInitialValue(initial),
		WithOnEdit(func(string) { l.comp = completion{at: -1}; l.list.SetText("") }),
		WithOnSubmit(l.submit))
	body := tui.NewFlex(tui.Vertical)
	body.Add(l.field)
	body.Add(l.list)
	var m *Modal
	m = NewModal(body,
		WithModalTitle(commandLineTitle),
		WithModalWidth(commandLineWidth),
		WithModalFooter(commandLineHelp),
		WithModalKeys(l.keys),
		WithOnDismiss(func(DismissReason) {
			if l.modal == m {
				l.modal, l.field, l.list = nil, nil, nil
			}
		}))
	if err := m.Open(host); err != nil {
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
	if err := l.core.RunCommand(line); err != nil {
		l.open(line, err)
	}
}

// keys are the prompt's own: Tab and Shift+Tab complete.
func (l *CommandLine) keys(k tui.KeyEvent) bool {
	if k.Code != tui.KeyTab || l.field == nil {
		return false
	}
	l.complete(k.Mods&tui.ModShift != 0)
	return true
}

// complete completes the verb being typed. The first Tab fills in what every candidate shares
// (and a space when there is one candidate) and lists them; each Tab after that takes the next
// candidate in turn, Shift+Tab the previous. Only the verb completes: once the line has a space,
// Tab lists the verb's arguments.
func (l *CommandLine) complete(back bool) {
	line := l.field.Value()
	if l.comp.cands == nil {
		verb, _, spaced := strings.Cut(line, " ")
		if spaced {
			if cmd, ok := l.core.Commands().Lookup(strings.TrimSuffix(verb, "!")); ok {
				l.list.SetText(describe(cmd))
			}
			return
		}
		cands := l.core.Commands().Complete(line)
		if len(cands) == 0 {
			l.list.SetText("no command starts with " + quoteOrNothing(line))
			return
		}
		l.comp = completion{typed: line, cands: cands, at: -1}
		l.list.SetText(describeAll(cands))
		if len(cands) == 1 {
			l.setField(cands[0].Name + " ")
			l.comp.cands = nil // done: the next Tab lists its arguments
			return
		}
		if p := commonPrefix(cands, line); p != line {
			l.setField(p)
			return
		}
	}
	n := len(l.comp.cands)
	switch {
	case l.comp.at < 0 && back:
		l.comp.at = n - 1
	case l.comp.at < 0:
		l.comp.at = 0
	case back:
		l.comp.at = (l.comp.at + n - 1) % n
	default:
		l.comp.at = (l.comp.at + 1) % n
	}
	l.setField(l.comp.cands[l.comp.at].Name)
}

// setField puts s in the field. SetValue is not typing (WithOnEdit does not run), so the
// completion and its list stay.
func (l *CommandLine) setField(s string) { l.field.SetValue(s) }

// describe is one candidate's line: "replace <term> <with> — replace each match, …".
func describe(c Command) string {
	s := c.Name
	if c.Args != "" {
		s += " " + c.Args
	}
	if c.Help != "" {
		s += " — " + c.Help
	}
	return s
}

func describeAll(cs []Command) string {
	lines := make([]string, len(cs))
	for i, c := range cs {
		lines[i] = describe(c)
	}
	return strings.Join(lines, "\n")
}

// commonPrefix is the longest prefix every candidate's name shares, at least typed.
func commonPrefix(cs []Command, typed string) string {
	p := cs[0].Name
	for _, c := range cs[1:] {
		for !strings.HasPrefix(c.Name, p) {
			p = p[:len(p)-1]
		}
	}
	if len(p) < len(typed) {
		return typed
	}
	return p
}

func quoteOrNothing(s string) string {
	if s == "" {
		return "that"
	}
	return "\"" + s + "\""
}

// replaceQuestion is the card replace asks with.
const replaceQuestion = "Replace this match?  y yes · n no · q stop"

// askReplace asks at each match of s, one card at a time, answered by y, n, q and Esc.
func (l *CommandLine) askReplace(s *ReplaceSession) error {
	host, ok := l.host()
	if !ok || !l.on {
		// nowhere to ask, or no prompt: every match, as with no widget
		for more := true; more; {
			var err error
			if more, err = s.Answer(true); err != nil {
				return err
			}
		}
		return nil
	}
	var m *Modal
	m = NewModal(NewText(replaceQuestion),
		WithModalTitle("replace"),
		WithPlacement(PlacementBottomRight),
		WithScrim(false),
		WithModalKeys(func(k tui.KeyEvent) bool {
			switch k.Code {
			case 'y', 'Y', 'n', 'N':
				more, err := s.Answer(k.Code == 'y' || k.Code == 'Y')
				if err != nil || !more {
					m.Dismiss(DismissAccept)
				}
				return true
			case 'q', 'Q':
				m.Dismiss(DismissCancel)
				return true
			}
			return false
		}),
		WithOnDismiss(func(DismissReason) {
			s.Stop()
			if l.ask == m {
				l.ask = nil
			}
		}))
	if err := m.Open(host); err != nil {
		s.Stop()
		return err
	}
	l.ask = m
	return nil
}
