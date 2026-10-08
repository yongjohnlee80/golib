package mermaid

import (
	"strconv"
	"strings"
)

// SequenceDiagram is a parsed sequence diagram: its participants, left to right, and its steps,
// top to bottom.
type SequenceDiagram struct {
	Title        string
	Participants []Participant // declared or first mentioned, in that order
	Steps        []Step        // in source order
}

func (*SequenceDiagram) Kind() Kind { return Sequence }

// Participant is a lifeline: a box (or a stick figure, for an actor) at its top and bottom.
type Participant struct {
	ID    string
	Label string // the ID when none is given
	Actor bool
	Span  [2]int
}

// StepKind is what a step of a sequence diagram is.
type StepKind uint8

const (
	StepMessage    StepKind = iota + 1
	StepNote                // over, left of or right of participants
	StepActivate            // From activates
	StepDeactivate          // From deactivates
	StepBlock               // a frame opens: loop, alt, opt, par, critical, break or rect
	StepBranch              // the open frame's next section: else, and, option
	StepEnd                 // the open frame closes
	StepNumber              // autonumber turns on (Start, Increment) or off
)

// SeqEnd is what a message ends in.
type SeqEnd uint8

const (
	SeqNone  SeqEnd = iota // -> and -->
	SeqArrow               // ->> and -->>
	SeqCross               // -x and --x
	SeqAsync               // -) and --): an open arrow
)

// NotePlace is where a note stands.
type NotePlace uint8

const (
	LeftOf NotePlace = iota + 1
	RightOf
	Over
)

// BlockKind is a frame's kind; its keyword is its name.
type BlockKind uint8

const (
	Loop BlockKind = iota + 1
	Alt
	Opt
	Par
	Critical
	Break
	RectBlock // rect: a coloured background, no frame
)

var blockNames = [...]string{Loop: "loop", Alt: "alt", Opt: "opt", Par: "par", Critical: "critical", Break: "break", RectBlock: "rect"}

func (b BlockKind) String() string { return enumName(blockNames[:], int(b)) }

// branchOf is the keyword that starts a block's next section; none for the others.
var branchOf = map[BlockKind]string{Alt: "else", Par: "and", Critical: "option"}

// Step is one statement of a sequence diagram. Which fields are set depends on Kind.
type Step struct {
	Kind StepKind
	// A message's sender and receiver; a note's first and last participant (the same for one);
	// the participant an activate or deactivate names, in From.
	From, To string
	// A message's, a note's, a frame's or a section's text; a rect's colour, as written.
	Text string
	Line LineKind // a message's: Solid or Dotted
	Head SeqEnd   // at To
	Tail SeqEnd   // at From: SeqArrow for <<->> and <<-->>, else SeqNone
	// Activate (+ before the receiver) activates To; Deactivate (-) deactivates From, the sender:
	// a reply that ends the call it answers.
	Activate, Deactivate bool
	Place                NotePlace
	Block                BlockKind // a StepBlock's, a StepBranch's and a StepEnd's frame
	// StepNumber: the first number and the step between numbers; Off turns numbering off.
	Start, Increment int
	Off              bool
	Span             [2]int
}

// sequence parses a sequence diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) sequence(rest string, restOff int) (Diagram, error) {
	s := &seqParser{p: p, d: &SequenceDiagram{}, index: map[string]int{}, active: map[string]int{}}
	if r := strings.TrimSpace(rest); r != "" && !strings.HasPrefix(r, "%%") {
		p.fail(restOff, "unexpected %q after sequenceDiagram", r)
	}
	for _, st := range seqStatements(p.src, restOff+len(rest)) {
		s.statement(st)
		if p.unsupported != nil {
			break
		}
	}
	if n := len(s.open); n > 0 {
		p.fail(s.open[n-1].off, "%s has no end", s.open[n-1].kind)
	}
	return p.done(s.d)
}

type seqParser struct {
	p      *parser
	d      *SequenceDiagram
	index  map[string]int // participant ID → index
	active map[string]int // activations open on each participant
	open   []openBlock    // frames open, outermost first
}

type openBlock struct {
	kind BlockKind
	off  int // where it opens
}

// seqStatements splits src[from:] into statements at newlines and semicolons; a statement
// starting with %% is a comment.
func seqStatements(src string, from int) []stmt {
	var out []stmt
	start := from
	flush := func(end int) {
		text := src[start:end]
		lead := len(text) - len(strings.TrimLeft(text, " \t\r"))
		if t := strings.TrimSpace(text); t != "" && !strings.HasPrefix(t, "%%") {
			out = append(out, stmt{t, start + lead})
		}
	}
	for i := from; i < len(src); i++ {
		if src[i] == '\n' || src[i] == ';' {
			flush(i)
			start = i + 1
		}
	}
	flush(len(src))
	return out
}

func (s *seqParser) statement(st stmt) {
	word, rest := firstWord(st.text)
	lower := strings.ToLower(word)
	if t, ok := strings.CutPrefix(lower, "title:"); ok { // title:Text, with no space
		word, lower, rest = "title", "title", st.text[len(st.text)-len(rest)-len(t):]
	}
	restOff := st.off + len(st.text) - len(rest)
	switch lower {
	case "participant", "actor":
		s.declare(rest, restOff, lower == "actor", st)
		return
	case "note":
		s.note(rest, restOff, st)
		return
	case "activate", "deactivate":
		id := strings.TrimSpace(rest)
		if id == "" {
			s.p.fail(restOff, "%s names no participant", lower)
			return
		}
		s.mention(id, st)
		k := StepActivate
		if lower == "deactivate" {
			k = StepDeactivate
			if !s.deactivate(id, st.off) {
				return
			}
		} else {
			s.active[id]++
		}
		s.d.Steps = append(s.d.Steps, Step{Kind: k, From: id, Span: span(st)})
		return
	case "autonumber":
		s.autonumber(rest, restOff, st)
		return
	case "title":
		s.d.Title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ":"))
		return
	case "loop", "alt", "opt", "par", "critical", "break", "rect":
		b := blockKind(lower)
		text := strings.TrimSpace(rest)
		if b != RectBlock {
			t, err := s.p.label(text, restOff)
			if err != nil {
				return
			}
			text = t
		}
		s.open = append(s.open, openBlock{b, st.off})
		s.d.Steps = append(s.d.Steps, Step{Kind: StepBlock, Block: b, Text: text, Span: span(st)})
		return
	case "else", "and", "option":
		n := len(s.open)
		if n == 0 || branchOf[s.open[n-1].kind] != lower {
			s.p.fail(st.off, "%q outside the frame it divides", lower)
			return
		}
		t, err := s.p.label(strings.TrimSpace(rest), restOff)
		if err != nil {
			return
		}
		s.d.Steps = append(s.d.Steps, Step{Kind: StepBranch, Block: s.open[n-1].kind, Text: t, Span: span(st)})
		return
	case "end":
		if strings.TrimSpace(rest) != "" {
			break // "end" naming a participant, in a message
		}
		n := len(s.open)
		if n == 0 {
			s.p.fail(st.off, "end with no frame open")
			return
		}
		s.d.Steps = append(s.d.Steps, Step{Kind: StepEnd, Block: s.open[n-1].kind, Span: span(st)})
		s.open = s.open[:n-1]
		return
	case "box", "create", "destroy", "link", "links", "properties", "details":
		s.p.unsupport(st.off, word+" statements")
		return
	}
	s.message(st)
}

func blockKind(w string) BlockKind {
	for k, n := range blockNames {
		if n == w {
			return BlockKind(k)
		}
	}
	return 0
}

func span(st stmt) [2]int { return [2]int{st.off, st.off + len(st.text)} }

// declare is a participant or actor statement: `participant A`, `actor B as Bob`.
func (s *seqParser) declare(rest string, off int, actor bool, st stmt) {
	rest = strings.TrimSpace(rest)
	if strings.Contains(rest, "@{") {
		s.p.unsupport(st.off, "participant types")
		return
	}
	id, label, aliased := strings.Cut(rest, " as ")
	id = strings.TrimSpace(id)
	if id == "" {
		s.p.fail(off, "a participant needs a name")
		return
	}
	if !aliased {
		label = id
	}
	text, err := s.p.label(strings.TrimSpace(label), off)
	if err != nil {
		return
	}
	s.mention(id, st)
	pt := &s.d.Participants[s.index[id]]
	pt.Label, pt.Actor, pt.Span = text, actor, span(st)
}

// mention adds a participant at its first mention.
func (s *seqParser) mention(id string, st stmt) {
	if _, ok := s.index[id]; ok {
		return
	}
	s.index[id] = len(s.d.Participants)
	s.d.Participants = append(s.d.Participants, Participant{ID: id, Label: id, Span: span(st)})
}

func (s *seqParser) deactivate(id string, off int) bool {
	if s.active[id] == 0 {
		s.p.fail(off, "%s is not active", id)
		return false
	}
	s.active[id]--
	return true
}

// note is `Note left of A: text`, `Note right of A: text`, `Note over A: text` or
// `Note over A,B: text`.
func (s *seqParser) note(rest string, off int, st stmt) {
	head, text, ok := strings.Cut(rest, ":")
	if !ok {
		s.p.fail(off, "a note needs ': text'")
		return
	}
	head = strings.TrimSpace(head)
	lower := strings.ToLower(head)
	var place NotePlace
	var who string
	switch {
	case strings.HasPrefix(lower, "left of "):
		place, who = LeftOf, head[len("left of "):]
	case strings.HasPrefix(lower, "right of "):
		place, who = RightOf, head[len("right of "):]
	case strings.HasPrefix(lower, "over "):
		place, who = Over, head[len("over "):]
	default:
		s.p.fail(off, "a note is left of, right of or over a participant")
		return
	}
	from, to, two := strings.Cut(who, ",")
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if !two {
		to = from
	}
	if from == "" || to == "" || (two && place != Over) {
		s.p.fail(off, "a note names one participant, or two when over")
		return
	}
	t, err := s.p.label(text, off+len(rest)-len(text))
	if err != nil {
		return
	}
	s.mention(from, st)
	s.mention(to, st)
	s.d.Steps = append(s.d.Steps, Step{Kind: StepNote, From: from, To: to, Text: t, Place: place, Span: span(st)})
}

// autonumber is `autonumber`, `autonumber off`, or `autonumber <start> [<increment>]`.
func (s *seqParser) autonumber(rest string, off int, st stmt) {
	f := strings.Fields(rest)
	step := Step{Kind: StepNumber, Start: 1, Increment: 1, Span: span(st)}
	switch {
	case len(f) == 1 && f[0] == "off":
		step.Off = true
	case len(f) <= 2:
		for i, w := range f {
			n, err := strconv.Atoi(w)
			if err != nil || n < 0 {
				s.p.fail(off, "autonumber takes numbers, or off")
				return
			}
			if i == 0 {
				step.Start = n
			} else {
				step.Increment = n
			}
		}
	default:
		s.p.fail(off, "autonumber takes at most a start and an increment")
		return
	}
	s.d.Steps = append(s.d.Steps, step)
}

type seqArrow struct {
	text       string
	line       LineKind
	head, tail SeqEnd
}

// seqArrows are the message arrows, longest first so a prefix never matches a longer arrow.
var seqArrows = []seqArrow{
	{"<<-->>", Dotted, SeqArrow, SeqArrow},
	{"<<->>", Solid, SeqArrow, SeqArrow},
	{"-->>", Dotted, SeqArrow, SeqNone},
	{"->>", Solid, SeqArrow, SeqNone},
	{"--x", Dotted, SeqCross, SeqNone},
	{"-x", Solid, SeqCross, SeqNone},
	{"--)", Dotted, SeqAsync, SeqNone},
	{"-)", Solid, SeqAsync, SeqNone},
	{"-->", Dotted, SeqNone, SeqNone},
	{"->", Solid, SeqNone, SeqNone},
}

// message is `A->>B: text`: a sender, an arrow, + or - perhaps, a receiver, and text after a
// colon (none is allowed). Anything else is a statement golib does not know.
func (s *seqParser) message(st stmt) {
	t := st.text
	at := strings.IndexAny(t, "-<")
	if at <= 0 || (t[at] == '<' && !strings.HasPrefix(t[at:], "<<-")) {
		s.p.unsupport(st.off, "the statement "+quoted(t))
		return
	}
	from := strings.TrimSpace(t[:at])
	var arrow *seqArrow
	for i := range seqArrows {
		if strings.HasPrefix(t[at:], seqArrows[i].text) {
			arrow = &seqArrows[i]
			break
		}
	}
	if arrow == nil {
		s.p.fail(st.off+at, "unknown arrow")
		return
	}
	step := Step{Kind: StepMessage, From: from, Line: arrow.line, Head: arrow.head, Tail: arrow.tail, Span: span(st)}
	r := strings.TrimLeft(t[at+len(arrow.text):], " ")
	switch {
	case strings.HasPrefix(r, "+"):
		step.Activate, r = true, r[1:]
	case strings.HasPrefix(r, "-"):
		step.Deactivate, r = true, r[1:]
	}
	to, text, _ := strings.Cut(r, ":")
	step.To = strings.TrimSpace(to)
	if from == "" || step.To == "" || strings.ContainsAny(step.To, "<>") {
		s.p.fail(st.off+at, "a message runs from one participant to another")
		return
	}
	label, err := s.p.label(text, st.off+len(t)-len(text))
	if err != nil {
		return
	}
	step.Text = label
	s.mention(from, st)
	s.mention(step.To, st)
	if step.Activate {
		s.active[step.To]++
	}
	if step.Deactivate && !s.deactivate(from, st.off) {
		return
	}
	s.d.Steps = append(s.d.Steps, step)
}

// quoted is s, quoted and cut short, for a message.
func quoted(s string) string {
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40]) + "…"
	}
	return strconv.Quote(s)
}
