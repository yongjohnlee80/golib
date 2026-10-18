package widget

import (
	"strings"
	"unicode/utf8"

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
	owner tui.Component // the widget it opens over
	core  *EditorCore

	modal *Modal          // the open prompt, nil when closed
	field *TextInput      // its field
	list  *completionList // the dropdown under the field: the candidates, or the error
	comp  completion      // Tab's state over the open prompt

	ask *Modal // replace's open question, nil when none
}

// completion is Tab's walk through the candidates: what was typed when it began, what precedes
// every candidate in the field (the verb and the words before an argument; "" for a verb), each
// candidate and its row in the dropdown, and which one the last Tab chose (-1: only the common
// prefix so far).
type completion struct {
	typed string
	head  string
	cands []string
	rows  []string
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
func cmdlineStep(ctx *tui.Context, k tui.KeyEvent, dropdown bool) (back, ok bool) {
	cmd, ok := ctx.KeyCommand(k, cmdlineCommands...)
	if !ok || !dropdown && (cmd.ID == CmdlineNext.ID || cmd.ID == CmdlinePrev.ID) {
		return false, false
	}
	return cmd.ID == CmdlineCompletePrev.ID || cmd.ID == CmdlinePrev.ID, true
}

// replaceAnswer is the replace question's answer k is: "yes", "no", "quit", or "" for none.
func replaceAnswer(ctx *tui.Context, k tui.KeyEvent) string {
	cmd, ok := ctx.KeyCommand(k, cmdlineReplaceCommands...)
	if !ok {
		return ""
	}
	return strings.TrimPrefix(cmd.ID, "tui.cmdline.replace.")
}

// complete completes the word being typed: the verb, from the core's Commands, or after it an
// argument, from the command's Complete. The first Tab fills in what every candidate shares (and a
// space when there is one candidate) and lists them; each Tab after that takes the next candidate
// in turn, Shift+Tab the previous. After the verb of a command without Complete, Tab lists its
// arguments and help.
func (l *CommandLine) complete(back bool) {
	line := l.field.Value()
	if l.comp.cands == nil {
		verb, rest, spaced := strings.Cut(line, " ")
		if spaced {
			l.completeArg(line, verb, rest, back)
			return
		}
		cmds := l.core.Commands().Complete(line)
		if len(cmds) == 0 {
			l.list.say("no command starts with " + quoteOrNothing(line))
			return
		}
		names := make([]string, len(cmds))
		for i, c := range cmds {
			names[i] = c.Name
		}
		if !l.offer(completion{typed: line, cands: names, rows: describeEach(cmds)}, describe(cmds[0])) {
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
	l.setField(l.comp.head + l.comp.cands[l.comp.at])
}

// offer starts Tab's walk through c's candidates, typed being c.typed's word. One candidate is
// filled in whole, with a space, and once is said: done, offer says false. Several are listed,
// and what they share is filled in: offer says false when that moved the field, true when the walk
// takes the first Tab on to the first candidate.
func (l *CommandLine) offer(c completion, once string) bool {
	c.at = -1
	l.comp = c
	if len(c.cands) == 1 {
		l.list.say(once)
		l.setField(c.head + c.cands[0] + " ")
		l.comp.cands = nil // done: the next Tab completes the next word
		return false
	}
	l.list.set(c.rows, -1)
	typed := strings.TrimPrefix(c.typed, c.head)
	if p := commonPrefix(c.cands, typed); p != typed {
		l.setField(c.head + p)
		return false
	}
	return true
}

// completeArg completes the argument being typed after verb: rest is the line after the verb's
// space. A command without Complete, or whose candidates none starts with the word, has Tab say
// its arguments and help instead.
func (l *CommandLine) completeArg(line, verb, rest string, back bool) {
	name := strings.TrimSuffix(verb, "!")
	cmd, ok := l.core.Commands().Lookup(name)
	if !ok {
		return
	}
	if cmd.Complete == nil {
		l.list.say(describe(cmd))
		return
	}
	words := strings.Fields(rest)
	word := ""
	if len(words) > 0 && !strings.HasSuffix(rest, " ") {
		word, words = words[len(words)-1], words[:len(words)-1]
	}
	seen := map[string]bool{}
	var cands []string
	for _, c := range cmd.Complete(CompleteContext{Core: l.core, Name: name, Bang: name != verb, Args: words, Word: word}) {
		if c != "" && strings.HasPrefix(c, word) && !seen[c] {
			seen[c] = true
			cands = append(cands, c)
		}
	}
	if len(cands) == 0 {
		l.list.say(describe(cmd))
		return
	}
	head := line[:len(line)-len(word)]
	if l.offer(completion{typed: line, head: head, cands: cands, rows: cands}, describe(cmd)) {
		l.complete(back) // several that share nothing more: this Tab takes the first (or the last)
	}
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
func commonPrefix(cs []string, typed string) string {
	p := cs[0]
	for _, c := range cs[1:] {
		for !strings.HasPrefix(c, p) {
			_, size := utf8.DecodeLastRuneInString(p) // a script's arguments need not be ASCII
			p = p[:len(p)-size]
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
			switch a := replaceAnswer(m.Context(), k); a {
			case "yes", "no":
				more, err := s.Answer(a == "yes")
				if err != nil || !more {
					m.Dismiss(DismissAccept)
				}
				return true
			case "quit":
				m.Dismiss(DismissCancel)
				return true
			}
			return false
		}),
		WithModalKeyBinder(func(km *keymap.Map) []keymap.Binding {
			return tui.BindingsOf(km, cmdlineReplaceCommands...)
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
