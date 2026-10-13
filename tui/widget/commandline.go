package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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

	modal *Modal          // the open prompt, nil when closed
	field *TextInput      // its field
	list  *completionList // the dropdown under the field: the candidates, or the error
	comp  completion      // Tab's state over the open prompt

	ask *Modal // replace's open question, nil when none
}

// completion is Tab's walk through the candidates: what was typed when it began, and which
// candidate the last Tab chose (-1: only the common prefix so far).
type completion struct {
	typed string
	cands []Command
	at    int
}

// commandLineWidth is the prompt's width in cells, frame included: room for a candidate's help.
const commandLineWidth = 64

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
	l.list = &completionList{at: -1}
	if failed != nil {
		// the package's sentinels name it for a log; the user reads only what went wrong
		l.list.say(strings.TrimPrefix(failed.Error(), "widget: "))
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
		WithModalWidth(commandLineWidth),
		WithModalRule(true),
		WithModalKeys(l.keys),
		WithOnDismiss(func(r DismissReason) {
			if l.modal == m {
				l.modal, l.field, l.list = nil, nil, nil
			}
			if r != DismissAccept {
				l.core.CommandLineClosed() // cancelled; an accepted line closes once it has run
			}
		}))
	m.SetFooterMessage(tui.Msg("tui.cmdline.help"))
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
	if err := l.core.RunCommand(line); err != nil && l.open(line, err) {
		return // reopened on the failure: still open
	}
	l.core.CommandLineClosed()
}

// keys are the prompt's own: Tab and Shift+Tab complete; with the dropdown open, Down and Up move
// through it as Tab and Shift+Tab do.
func (l *CommandLine) keys(k tui.KeyEvent) bool {
	if l.field == nil {
		return false
	}
	switch {
	case k.Code == tui.KeyTab:
		l.complete(k.Mods&tui.ModShift != 0)
		return true
	case (k.Code == tui.KeyDown || k.Code == tui.KeyUp) && len(l.comp.cands) > 1:
		l.complete(k.Code == tui.KeyUp)
		return true
	}
	return false
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
				l.list.say(describe(cmd))
			}
			return
		}
		cands := l.core.Commands().Complete(line)
		if len(cands) == 0 {
			l.list.say("no command starts with " + quoteOrNothing(line))
			return
		}
		l.comp = completion{typed: line, cands: cands, at: -1}
		if len(cands) == 1 {
			l.list.say(describe(cands[0]))
			l.setField(cands[0].Name + " ")
			l.comp.cands = nil // done: the next Tab lists its arguments
			return
		}
		l.list.set(describeEach(cands), -1)
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
	l.list.choose(l.comp.at)
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

func describeEach(cs []Command) []string {
	rows := make([]string, len(cs))
	for i, c := range cs {
		rows[i] = describe(c)
	}
	return rows
}

// completionList is the prompt's dropdown under the field: one candidate a row in a framed box, the
// one Tab chose highlighted, at most completionRows of them in view (the view follows the chosen
// one); or one message on its own (an error, a verb's arguments), wrapped and unframed. Empty, it
// takes no room.
type completionList struct {
	Base
	rows []string
	at   int    // the chosen row; -1 for none yet
	msg  string // shown in place of rows when set
}

// completionRows is how many candidates the dropdown shows at once.
const completionRows = 8

func (c *completionList) set(rows []string, at int) {
	c.rows, c.at, c.msg = rows, at, ""
	c.RequestLayout()
	c.MarkDirty()
}

func (c *completionList) choose(at int) {
	c.at = at
	c.MarkDirty()
}

func (c *completionList) say(msg string) {
	c.rows, c.at, c.msg = nil, -1, msg
	c.RequestLayout()
	c.MarkDirty()
}

func (c *completionList) clear() {
	if c.msg != "" || c.rows != nil {
		c.set(nil, -1)
	}
}

// Layout is the field's width, and a row for each candidate in view, or the message's lines.
func (c *completionList) Layout(cs tui.Constraints) tui.Size {
	w := cs.MaxW
	if w == tui.Unbounded {
		w = 0
		for _, r := range c.rows {
			w = max(w, c.measure(r))
		}
		w = max(w, c.measure(c.msg))
	}
	h := min(len(c.rows), completionRows)
	switch {
	case c.msg != "":
		h = len(wrapLine(c.msg, max(w, 1), c.measure))
	case h > 0:
		h += 2 // the frame
		if cs.MaxW == tui.Unbounded {
			w += 2
		}
	}
	return cs.Constrain(tui.Size{W: w, H: h})
}

// first is the first candidate in view: the chosen one is always in it.
func (c *completionList) first(h int) int {
	if c.at < h {
		return 0
	}
	return min(c.at-h+1, len(c.rows)-h)
}

// Render paints the rows on the card's colours, the chosen one as a selection.
func (c *completionList) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	card := cardText()
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", card)
	if c.msg != "" {
		for y, ln := range wrapLine(c.msg, sz.W, s.StringWidth) {
			if y >= sz.H {
				break
			}
			drawText(s, 0, y, ln, card)
		}
		return
	}
	if len(c.rows) == 0 || sz.W < 3 || sz.H < 3 {
		return
	}
	// the frame, on the card's colours with the theme's border
	frame := card.Foreground(style.TokenBorder)
	right, bottom := sz.W-1, sz.H-1
	s.SetCell(0, 0, "┌", frame)
	s.SetCell(right, 0, "┐", frame)
	s.SetCell(0, bottom, "└", frame)
	s.SetCell(right, bottom, "┘", frame)
	for x := 1; x < right; x++ {
		s.SetCell(x, 0, "─", frame)
		s.SetCell(x, bottom, "─", frame)
	}
	for y := 1; y < bottom; y++ {
		s.SetCell(0, y, "│", frame)
		s.SetCell(right, y, "│", frame)
	}
	inner := s.Sub(tui.Rect{X: 1, Y: 1, W: sz.W - 2, H: sz.H - 2})
	iw, ih := sz.W-2, sz.H-2
	chosen := style.New().Background(style.TokenPrimary).Foreground(style.TokenTextOnPrimary)
	top := c.first(ih)
	for y := 0; y < ih && top+y < len(c.rows); y++ {
		i := top + y
		st := card
		if i == c.at {
			st = chosen
			inner.Fill(tui.Rect{Y: y, W: iw, H: 1}, " ", st)
		}
		drawText(inner, 0, y, truncate(c.rows[i], iw, inner.StringWidth), st)
	}
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
	question := NewText("", WithTextStyle(cardText()))
	question.SetTextMessage(tui.Msg("tui.cmdline.replaceQuestion"))
	m = NewModal(question,
		WithModalTitleMessage(tui.Msg("tui.cmdline.replaceTitle")),
		WithPlacement(PlacementTopRight), // clear of the notifications at the bottom
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

// cardText is a Text's look on the prompt's card: the card's own colours. A Text with none would
// clear its cells to the screen's default background, a dark block on a themed card.
func cardText() style.Style {
	return style.New().Background(style.TokenPanel).Foreground(style.TokenForeground)
}
