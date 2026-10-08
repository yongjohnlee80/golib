package mermaid

import (
	"errors"
	"slices"
	"strings"
)

// StateKind is what a state is drawn as.
type StateKind uint8

const (
	StateNormal StateKind = iota
	StateStart            // [*] where a transition leaves it: its scope's start
	StateEnd              // [*] where a transition reaches it: its scope's end
	StateFork             // <<fork>>
	StateJoin             // <<join>>
	StateChoice           // <<choice>>
)

var stateKindNames = [...]string{StateNormal: "normal", StateStart: "start", StateEnd: "end", StateFork: "fork",
	StateJoin: "join", StateChoice: "choice"}

func (k StateKind) String() string               { return enumName(stateKindNames[:], int(k)) }
func (k StateKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// StateDiagram is a parsed state diagram. A composite state is a scope of its own: every state,
// transition and note belongs to exactly one scope, and a transition joins two states of the
// scope it is written in (Mermaid draws none across composites).
type StateDiagram struct {
	Dir         Dir
	States      []StateNode // in order of first mention; each scope's start and end included
	Transitions []Transition
	Notes       []StateNote
	Classes     map[string]Style // classDef; "default" applies to every state
}

func (*StateDiagram) Kind() Kind { return State }

// StateNode is a state, a pseudo-state or a composite.
type StateNode struct {
	// ID is the state's id. A scope's start and end are "[*]start" and "[*]end", followed by
	// "@" and the composite's id inside one.
	ID string
	// Label is the state's name as drawn: its description in `state "…" as ID`, else its id.
	Label string
	// Descriptions are the `ID : text` lines, drawn under the name.
	Descriptions []string
	Kind         StateKind
	Composite    bool   // it has a body: a scope of its own
	Dir          Dir    // a composite's own direction; zero: its parent's
	Parent       string // the composite it is in; "" at top level
	Classes      []string
	Style        Style // resolved: classDef default, then each class, then style statements
	Span         [2]int
}

// Transition is a transition between two states of one scope.
type Transition struct {
	From, To string
	Label    string
	Span     [2]int
}

// StateNote is a note beside a state.
type StateNote struct {
	State string
	Side  NotePlace // LeftOf or RightOf
	Text  string
	Span  [2]int
}

// stateParser holds a state diagram's parse.
type stateParser struct {
	p       *parser
	d       *StateDiagram
	index   map[string]int
	scope   []string // open composites, outermost first
	classOf []classUse
	styles  map[string]Style
}

// stateLine is one line of a state diagram's body.
type stateLine struct {
	text string
	off  int
}

// state parses a state diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) state(rest string, restOff int) (Diagram, error) {
	sp := &stateParser{p: p, d: &StateDiagram{Dir: TB, Classes: map[string]Style{}}, index: map[string]int{},
		styles: map[string]Style{}}
	if r := strings.TrimSpace(rest); r != "" {
		p.fail(restOff+strings.Index(rest, r), "unexpected %q after stateDiagram", r)
	}
	lines := sp.lines(restOff + len(rest))
	for i := 0; i < len(lines); i++ {
		i = sp.statement(lines, i)
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	if len(sp.scope) > 0 {
		p.fail(len(p.src), "state %q has no closing }", sp.scope[len(sp.scope)-1])
	}
	sp.resolve()
	return p.done(sp.d)
}

// lines splits the body from off into trimmed lines, skipping blank ones and %% comments.
func (sp *stateParser) lines(off int) []stateLine {
	var out []stateLine
	s := sp.p.src
	for i := off; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		end := len(s)
		if j >= 0 {
			end = i + j
		}
		raw := s[i:end]
		if t := strings.TrimSpace(raw); t != "" && !strings.HasPrefix(t, "%%") {
			out = append(out, stateLine{t, i + strings.Index(raw, t)})
		}
		i = end + 1
	}
	return out
}

// here is the open scope: the innermost composite, "" at top level.
func (sp *stateParser) here() string {
	if len(sp.scope) == 0 {
		return ""
	}
	return sp.scope[len(sp.scope)-1]
}

// statement reads lines[i] (and the lines a note spans) and answers the last line it read.
func (sp *stateParser) statement(lines []stateLine, i int) int {
	ln := lines[i]
	w, rest := firstWord(ln.text)
	restOff := ln.off + len(ln.text) - len(rest)
	switch w {
	case "}":
		if rest != "" {
			sp.p.fail(restOff, "unexpected %q after }", rest)
		}
		if len(sp.scope) == 0 {
			sp.p.fail(ln.off, "} without a composite state")
			return i
		}
		sp.d.States[sp.index[sp.here()]].Span[1] = ln.off + 1
		sp.scope = sp.scope[:len(sp.scope)-1]
	case "--":
		sp.p.unsupport(ln.off, "concurrent regions")
	case "direction":
		d, ok := parseDir(rest)
		if !ok {
			sp.p.fail(restOff, "unknown direction %q", rest)
			return i
		}
		if len(sp.scope) == 0 {
			sp.d.Dir = d
			return i
		}
		sp.d.States[sp.index[sp.here()]].Dir = d
	case "state":
		sp.declare(rest, restOff, ln)
	case "note":
		return sp.note(lines, i, rest, restOff)
	case "classDef":
		names, props := firstWord(rest)
		style := sp.p.styleProps(props, ln.off+len(ln.text)-len(props))
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				sp.d.Classes[n] = merge(sp.d.Classes[n], style)
			}
		}
	case "class":
		// `class a, b name`: the class is the last word; the ids before it may hold spaces
		k := strings.LastIndexAny(rest, " \t")
		if k < 0 {
			sp.p.fail(restOff, "class wants state ids and one class name")
			return i
		}
		ids, cls := rest[:k], rest[k+1:]
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				sp.classOf = append(sp.classOf, classUse{id, cls})
			}
		}
	case "style":
		id, props := firstWord(rest)
		sp.styles[id] = merge(sp.styles[id], sp.p.styleProps(props, ln.off+len(ln.text)-len(props)))
	case "hide":
		sp.p.unsupport(ln.off, "hide empty description")
	case "click", "href", "call":
		sp.p.unsupport(ln.off, w)
	case "accTitle:", "accDescr:":
		// accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if !strings.HasPrefix(rest, ":") {
			sp.p.unsupport(ln.off, w+" block")
		}
	default:
		sp.transitionOrState(ln)
	}
	return i
}

// declare reads `state ID` and `state "Label" as ID`, either followed by <<fork>>, <<join>> or
// <<choice>>, :::class, `: description`, or `{` to open the state's body.
func (sp *stateParser) declare(rest string, off int, ln stateLine) {
	if rest == "" {
		sp.p.fail(off, "state wants an id")
		return
	}
	label, hasLabel := "", false
	cur := rest
	if strings.HasPrefix(cur, "\"") {
		q := strings.IndexByte(cur[1:], '"')
		if q < 0 {
			sp.p.fail(off, "the state's description is not closed")
			return
		}
		lbl, err := sp.p.label(cur[:q+2], off)
		if err != nil {
			return
		}
		label, hasLabel = lbl, true
		after := strings.TrimLeft(cur[q+2:], " \t")
		as, tail := firstWord(after)
		if as != "as" || tail == "" {
			sp.p.fail(off+len(rest)-len(after), "a state's description wants `as` and an id after it")
			return
		}
		cur = tail
	}
	at := off + len(rest) - len(cur)
	n := identLen(cur)
	if n == 0 {
		sp.p.fail(at, "state wants an id, not %q", cur)
		return
	}
	id := cur[:n]
	cur = strings.TrimLeft(cur[n:], " \t")
	kind, open := StateNormal, false
	var classes, descs []string
	for cur != "" {
		at := off + len(rest) - len(cur)
		switch {
		case strings.HasPrefix(cur, "<<"):
			e := strings.Index(cur, ">>")
			if e < 0 {
				sp.p.fail(at, "<< is not closed")
				return
			}
			switch cur[2:e] {
			case "fork":
				kind = StateFork
			case "join":
				kind = StateJoin
			case "choice":
				kind = StateChoice
			default:
				sp.p.fail(at, "unknown state type %q", cur[:e+2])
				return
			}
			cur = cur[e+2:]
		case strings.HasPrefix(cur, ":::"):
			n := identLen(cur[3:])
			if n == 0 {
				sp.p.fail(at, "::: wants a class name")
				return
			}
			classes = append(classes, cur[3:3+n])
			cur = cur[3+n:]
		case cur == "{":
			open, cur = true, ""
		case strings.HasPrefix(cur, ":"):
			desc, err := sp.p.label(cur[1:], at+1)
			if err != nil {
				return
			}
			if desc != "" {
				descs = append(descs, desc)
			}
			cur = ""
		default:
			sp.p.fail(at, "unexpected %q in a state declaration", cur)
			return
		}
		cur = strings.TrimLeft(cur, " \t")
	}
	if open && kind != StateNormal {
		sp.p.fail(off, "a %v state has no body", kind)
		return
	}
	i, ok := sp.mention(id, ln.off, ln.off+len(ln.text))
	if !ok {
		return
	}
	st := &sp.d.States[i]
	if hasLabel {
		st.Label = label
	}
	if kind != StateNormal {
		st.Kind = kind
	}
	st.Descriptions = append(st.Descriptions, descs...)
	sp.addClasses(id, classes)
	if open {
		st.Composite = true
		sp.scope = append(sp.scope, id)
	}
}

// note reads `note left of ID : text`, or `note right of ID` and the lines up to `end note`.
// It answers the last line it read.
func (sp *stateParser) note(lines []stateLine, i int, rest string, off int) int {
	ln := lines[i]
	if strings.HasPrefix(rest, "\"") {
		sp.p.unsupport(ln.off, "a floating note")
		return i
	}
	side, tail := firstWord(rest)
	of, tail := firstWord(tail)
	var sd NotePlace
	switch {
	case side == "right" && of == "of":
		sd = RightOf
	case side == "left" && of == "of":
		sd = LeftOf
	default:
		sp.p.fail(off, "a note wants `left of` or `right of` a state")
		return i
	}
	tailOff := ln.off + len(ln.text) - len(tail)
	n := identLen(tail)
	if n == 0 {
		sp.p.fail(tailOff, "a note wants a state's id")
		return i
	}
	id := tail[:n]
	after := strings.TrimLeft(tail[n:], " \t")
	afterOff := ln.off + len(ln.text) - len(after)
	var text string
	last := i
	switch {
	case strings.HasPrefix(after, ":"):
		t, err := sp.p.label(after[1:], afterOff+1)
		if err != nil {
			return i
		}
		text = t
	case after != "":
		sp.p.fail(afterOff, "unexpected %q after the note's state", after)
		return i
	default:
		var body []string
		for last = i + 1; last < len(lines) && lines[last].text != "end note"; last++ {
			body = append(body, lines[last].text)
		}
		if last == len(lines) {
			sp.p.fail(ln.off, "the note has no end note")
			return last
		}
		t, err := sp.p.label(strings.Join(body, "\n"), lines[min(i+1, last)].off)
		if err != nil {
			return last
		}
		text = t
	}
	if _, ok := sp.mention(id, ln.off, ln.off+len(ln.text)); !ok {
		return last
	}
	end := lines[last].off + len(lines[last].text)
	sp.d.Notes = append(sp.d.Notes, StateNote{State: id, Side: sd, Text: text, Span: [2]int{ln.off, end}})
	return last
}

// transitionOrState reads `A --> B`, `A --> B : label`, `ID : description` and `ID`.
func (sp *stateParser) transitionOrState(ln stateLine) {
	t := ln.text
	k := strings.Index(t, "-->")
	if k < 0 {
		id, cls, rest, ok := sp.end(t, ln.off)
		if !ok {
			return
		}
		if id == "[*]" {
			sp.p.fail(ln.off, "[*] is a start or an end: it wants a transition")
			return
		}
		rest = strings.TrimLeft(rest, " \t")
		restOff := ln.off + len(t) - len(rest)
		var desc string
		switch {
		case rest == "":
		case strings.HasPrefix(rest, ":"):
			d, err := sp.p.label(rest[1:], restOff+1)
			if err != nil {
				return
			}
			desc = d
		default:
			sp.p.fail(restOff, "unexpected %q after state %q", rest, id)
			return
		}
		i, ok := sp.mention(id, ln.off, ln.off+len(t))
		if !ok {
			return
		}
		if desc != "" {
			sp.d.States[i].Descriptions = append(sp.d.States[i].Descriptions, desc)
		}
		sp.addClasses(id, cls)
		return
	}
	left := strings.TrimSpace(t[:k])
	from, fcls, frest, ok := sp.end(left, ln.off)
	if !ok {
		return
	}
	if frest != "" {
		sp.p.fail(ln.off+len(left)-len(frest), "unexpected %q before -->", frest)
		return
	}
	right := strings.TrimLeft(t[k+3:], " \t")
	rOff := ln.off + len(t) - len(right)
	if right == "" {
		sp.p.fail(ln.off+k+3, "--> wants a state after it")
		return
	}
	to, tcls, rrest, ok := sp.end(right, rOff)
	if !ok {
		return
	}
	rrest = strings.TrimLeft(rrest, " \t")
	rrestOff := ln.off + len(t) - len(rrest)
	var label string
	switch {
	case rrest == "":
	case strings.HasPrefix(rrest, ":"):
		lbl, err := sp.p.label(rrest[1:], rrestOff+1)
		if err != nil {
			return
		}
		label = lbl
	default:
		sp.p.fail(rrestOff, "unexpected %q after the transition", rrest)
		return
	}
	scope := sp.here()
	if from == "[*]" {
		from = pseudoID("[*]start", scope)
		sp.pseudo(from, StateStart, ln.off)
	} else if _, ok := sp.mention(from, ln.off, ln.off+len(left)); !ok {
		return
	}
	if to == "[*]" {
		to = pseudoID("[*]end", scope)
		sp.pseudo(to, StateEnd, rOff)
	} else if _, ok := sp.mention(to, rOff, rOff+len(right)-len(rrest)); !ok {
		return
	}
	sp.addClasses(from, fcls)
	sp.addClasses(to, tcls)
	sp.d.Transitions = append(sp.d.Transitions, Transition{From: from, To: to, Label: label, Span: [2]int{ln.off, ln.off + len(t)}})
}

// pseudoID is a scope's start or end: base, and the composite's id after "@" inside one.
func pseudoID(base, scope string) string {
	if scope == "" {
		return base
	}
	return base + "@" + scope
}

// end reads one end of a transition: [*] or an id, perhaps with :::classes. It answers the
// text after it.
func (sp *stateParser) end(s string, off int) (id string, classes []string, rest string, ok bool) {
	if strings.HasPrefix(s, "[*]") {
		return "[*]", nil, s[3:], true
	}
	n := identLen(s)
	if n == 0 {
		sp.p.fail(off, "a state's id, not %q", s)
		return "", nil, "", false
	}
	id, rest = s[:n], s[n:]
	for strings.HasPrefix(rest, ":::") {
		m := identLen(rest[3:])
		if m == 0 {
			sp.p.fail(off+len(s)-len(rest), "::: wants a class name")
			return "", nil, "", false
		}
		classes = append(classes, rest[3:3+m])
		rest = rest[3+m:]
	}
	return id, classes, rest, true
}

func (sp *stateParser) addClasses(id string, classes []string) {
	for _, c := range classes {
		sp.classOf = append(sp.classOf, classUse{id, c})
	}
}

// mention finds or adds state id in the open scope. A state of another scope is declined:
// Mermaid draws no transition across composites, nor to the composite a state is in.
func (sp *stateParser) mention(id string, from, to int) (int, bool) {
	scope := sp.here()
	if slices.Contains(sp.scope, id) {
		sp.p.unsupport(from, "a composite state used inside itself")
		return 0, false
	}
	if i, ok := sp.index[id]; ok {
		if sp.d.States[i].Parent != scope {
			sp.p.unsupport(from, "a state used across composite states")
			return 0, false
		}
		return i, true
	}
	sp.index[id] = len(sp.d.States)
	sp.d.States = append(sp.d.States, StateNode{ID: id, Label: id, Parent: scope, Span: [2]int{from, to}})
	return sp.index[id], true
}

// pseudo finds or adds the open scope's start or end.
func (sp *stateParser) pseudo(id string, kind StateKind, off int) {
	if _, ok := sp.index[id]; ok {
		return
	}
	sp.index[id] = len(sp.d.States)
	sp.d.States = append(sp.d.States, StateNode{ID: id, Kind: kind, Parent: sp.here(), Span: [2]int{off, off + 3}})
}

// resolve applies classes and styles.
func (sp *stateParser) resolve() {
	for _, cu := range sp.classOf {
		if i, ok := sp.index[cu.id]; ok {
			sp.d.States[i].Classes = append(sp.d.States[i].Classes, cu.class)
		}
	}
	for i := range sp.d.States {
		s := &sp.d.States[i]
		st := slices.Clone(sp.d.Classes["default"])
		for _, c := range s.Classes {
			st = merge(st, sp.d.Classes[c])
		}
		st = merge(st, sp.styles[s.ID])
		if len(st) > 0 {
			s.Style = st
		}
	}
}
