package mermaid

import (
	"errors"
	"regexp"
	"slices"
	"strings"
)

// RequirementDiagram is a parsed requirement diagram: requirements and elements, and the
// relationships between them.
type RequirementDiagram struct {
	Dir       Dir
	Nodes     []RequirementNode     // requirements and elements, in order of definition
	Relations []RequirementRelation // in source order
	Classes   map[string]Style
}

func (*RequirementDiagram) Kind() Kind { return Requirement }

// RequirementType is what a requirement diagram's node is: one of the requirement kinds, or an
// element.
type RequirementType uint8

const (
	ReqRequirement      RequirementType = iota + 1 // requirement
	ReqFunctional                                  // functionalRequirement
	ReqInterface                                   // interfaceRequirement
	ReqPerformance                                 // performanceRequirement
	ReqPhysical                                    // physicalRequirement
	ReqDesignConstraint                            // designConstraint
	ReqElement                                     // element
)

var reqTypeNames = [...]string{ReqRequirement: "requirement", ReqFunctional: "functionalRequirement",
	ReqInterface: "interfaceRequirement", ReqPerformance: "performanceRequirement", ReqPhysical: "physicalRequirement",
	ReqDesignConstraint: "designConstraint", ReqElement: "element"}

// reqStereotypes are the types as a diagram titles their boxes.
var reqStereotypes = [...]string{ReqRequirement: "Requirement", ReqFunctional: "Functional Requirement",
	ReqInterface: "Interface Requirement", ReqPerformance: "Performance Requirement", ReqPhysical: "Physical Requirement",
	ReqDesignConstraint: "Design Constraint", ReqElement: "Element"}

func (t RequirementType) String() string               { return enumName(reqTypeNames[:], int(t)) }
func (t RequirementType) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// Stereotype is the type as the box's title reads it: "Requirement", "Design Constraint",
// "Element".
func (t RequirementType) Stereotype() string { return enumName(reqStereotypes[:], int(t)) }

// RequirementRisk is a requirement's risk; zero when it gives none.
type RequirementRisk uint8

const (
	ReqRiskLow RequirementRisk = iota + 1
	ReqRiskMedium
	ReqRiskHigh
)

var reqRiskNames = [...]string{ReqRiskLow: "Low", ReqRiskMedium: "Medium", ReqRiskHigh: "High"}

func (r RequirementRisk) String() string               { return enumName(reqRiskNames[:], int(r)) }
func (r RequirementRisk) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// RequirementVerify is how a requirement is verified; zero when it gives none.
type RequirementVerify uint8

const (
	ReqVerifyAnalysis RequirementVerify = iota + 1
	ReqVerifyInspection
	ReqVerifyTest
	ReqVerifyDemonstration
)

var reqVerifyNames = [...]string{ReqVerifyAnalysis: "Analysis", ReqVerifyInspection: "Inspection",
	ReqVerifyTest: "Test", ReqVerifyDemonstration: "Demonstration"}

func (v RequirementVerify) String() string               { return enumName(reqVerifyNames[:], int(v)) }
func (v RequirementVerify) MarshalText() ([]byte, error) { return []byte(v.String()), nil }

// RequirementNode is a requirement (its ID, Text, Risk and Verify) or an element (its ElemType
// and DocRef), by Type.
type RequirementNode struct {
	Name     string
	Type     RequirementType
	ID, Text string // a requirement's
	Risk     RequirementRisk
	Verify   RequirementVerify
	ElemType string // an element's type
	DocRef   string // an element's docref
	Classes  []string
	// Style is resolved: classDef default, then each class, then style statements.
	Style Style
	Span  [2]int // its definition, up to its block's {
}

// RequirementRelKind is a relationship's kind.
type RequirementRelKind uint8

const (
	ReqContains RequirementRelKind = iota + 1
	ReqCopies
	ReqDerives
	ReqSatisfies
	ReqVerifies
	ReqRefines
	ReqTraces
)

var reqRelNames = [...]string{ReqContains: "contains", ReqCopies: "copies", ReqDerives: "derives",
	ReqSatisfies: "satisfies", ReqVerifies: "verifies", ReqRefines: "refines", ReqTraces: "traces"}

func (k RequirementRelKind) String() string               { return enumName(reqRelNames[:], int(k)) }
func (k RequirementRelKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// RequirementRelation is a relationship from Src to Dst: `Src - kind -> Dst`, also written
// `Dst <- kind - Src`.
type RequirementRelation struct {
	Src, Dst string
	Kind     RequirementRelKind
	Span     [2]int
}

// reqParser is the parse of one requirement diagram. It holds the parser rather than embedding it,
// so the parser's own statement and resolve stay the flowchart's.
type reqParser struct {
	p       *parser
	d       *RequirementDiagram
	index   map[string]int // node name → index in d.Nodes
	block   int            // the node whose { … } block is open; -1 when none
	blockAt int            // where that block's definition starts, for an unclosed block's error
	rels    []stmt         // each relationship's statement, read once every node is defined
	classOf []classUse
	styles  map[string]Style
}

// requirement parses a requirement diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) requirement(rest string, restOff int) (Diagram, error) {
	r := &reqParser{p: p, d: &RequirementDiagram{Dir: TB, Classes: map[string]Style{}}, index: map[string]int{},
		block: -1, styles: map[string]Style{}}
	body := restOff
	if t := strings.TrimSpace(rest); t != "" {
		if !strings.HasPrefix(t, ";") {
			r.p.fail(restOff+strings.Index(rest, t), "unexpected %q after requirementDiagram", t)
		}
		body = restOff + strings.Index(rest, t)
	}
	for _, st := range p.erStatements(body) {
		if r.block >= 0 {
			r.inBlock(st)
		} else {
			r.statement(st)
		}
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	if r.block >= 0 {
		r.p.fail(r.blockAt, "the block of %q has no }", r.d.Nodes[r.block].Name)
	}
	r.relations()
	r.resolve()
	return p.done(r.d)
}

// reqTypeOf is the node type a definition's keyword names.
func reqTypeOf(w string) (RequirementType, bool) {
	for t, n := range reqTypeNames {
		if n != "" && n == w {
			return RequirementType(t), true
		}
	}
	return 0, false
}

var (
	reqForward  = regexp.MustCompile(`^(.+?)\s*-\s*(\w+)\s*->\s*(.+)$`) // src - kind -> dst
	reqBackward = regexp.MustCompile(`^(.+?)\s*<-\s*(\w+)\s*-\s*(.+)$`) // dst <- kind - src
)

// statement is one statement outside a block: a keyword, a definition opening its block, or a
// relationship.
func (r *reqParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "direction":
		d, ok := parseDir(rest)
		if !ok {
			r.p.fail(restOff, "unknown direction %q", rest)
			return
		}
		r.d.Dir = d
		return
	case "classDef":
		names, props := firstWord(rest)
		style := r.p.styleProps(props, st.off+len(st.text)-len(props))
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				r.d.Classes[n] = merge(r.d.Classes[n], style)
			}
		}
		return
	case "class":
		ids, cls := firstWord(rest)
		if cls == "" || strings.ContainsAny(cls, " \t") {
			r.p.fail(restOff, "class wants node names and one class name")
			return
		}
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				r.classOf = append(r.classOf, classUse{id, cls})
			}
		}
		return
	case "style":
		id, props := firstWord(rest)
		r.styles[id] = merge(r.styles[id], r.p.styleProps(props, st.off+len(st.text)-len(props)))
		return
	case "click", "linkStyle", "call", "callback", "href", "title":
		r.p.unsupport(st.off, w)
		return
	case "accTitle:", "accDescr:":
		return // accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if strings.HasPrefix(rest, ":") {
			return
		}
		r.p.unsupport(st.off, w+" block")
		return
	}
	if t, ok := reqTypeOf(w); ok {
		r.define(t, rest, restOff, st)
		return
	}
	if strings.HasPrefix(st.text, "}") {
		r.p.fail(st.off, "} without a requirement's or an element's block")
		return
	}
	if reqForward.MatchString(st.text) || reqBackward.MatchString(st.text) {
		r.rels = append(r.rels, st)
		return
	}
	r.p.unsupport(st.off, "the statement "+quoted(st.text))
}

// define is `<type> name[:::class] {`, and perhaps fields and the } on the same line.
func (r *reqParser) define(t RequirementType, rest string, restOff int, st stmt) {
	brace := strings.IndexByte(rest, '{')
	if brace < 0 {
		r.p.fail(restOff, "%s wants a name and a { block }", t)
		return
	}
	head := strings.TrimSpace(rest[:brace])
	var cls string
	if i := strings.Index(head, ":::"); i >= 0 {
		head, cls = strings.TrimSpace(head[:i]), strings.TrimSpace(head[i+3:])
	}
	name, ok := r.name(head, restOff)
	if !ok {
		return
	}
	i, seen := r.index[name]
	if !seen {
		i = len(r.d.Nodes)
		r.index[name] = i
		r.d.Nodes = append(r.d.Nodes, RequirementNode{Name: name, Type: t, Span: [2]int{st.off, restOff + brace + 1}})
	} else if (r.d.Nodes[i].Type == ReqElement) != (t == ReqElement) {
		r.p.fail(st.off, "%q is defined as a requirement and as an element", name)
		return
	}
	if cls != "" {
		r.classOf = append(r.classOf, classUse{name, cls})
	}
	r.block, r.blockAt = i, st.off
	// what follows the { on its line: fields, up to a } that closes the block there
	after := rest[brace+1:]
	if t := strings.TrimSpace(after); t != "" {
		r.inBlock(stmt{t, restOff + brace + 1 + strings.Index(after, t)})
	}
}

// name reads a node's name: a word, or a quoted string.
func (r *reqParser) name(s string, off int) (string, bool) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		r.p.fail(off, "a requirement or an element needs a name")
		return "", false
	case strings.HasPrefix(s, `"`):
		if len(s) < 2 || !strings.HasSuffix(s, `"`) {
			r.p.fail(off, "the name %s has no closing quote", s)
			return "", false
		}
		n, err := r.p.label(s, off)
		return n, err == nil && n != ""
	case strings.ContainsAny(s, " \t\""):
		r.p.fail(off, "the name %q holds a space: quote it", s)
		return "", false
	}
	return s, true
}

// inBlock is one line inside a node's block: `field: value`, or the } that closes it (perhaps
// after a field on the same line).
func (r *reqParser) inBlock(st stmt) {
	text := strings.TrimSpace(st.text)
	// a } inside a quoted value is the value's
	closes := strings.HasSuffix(text, "}") && strings.Count(text, `"`)%2 == 0
	if closes {
		text = strings.TrimSpace(strings.TrimSuffix(text, "}"))
	}
	if text != "" {
		r.field(text, st.off)
	}
	if closes {
		r.block = -1
	}
}

// field is `key: value` in the open block: a requirement's id, text, risk and verifymethod, an
// element's type and docref.
func (r *reqParser) field(text string, off int) {
	key, value, ok := strings.Cut(text, ":")
	if !ok {
		r.p.fail(off, "a field is key: value")
		return
	}
	key = strings.ToLower(strings.TrimSpace(key))
	valueOff := off + len(text) - len(value)
	value = strings.TrimSpace(value)
	n := &r.d.Nodes[r.block]
	plain := func(into *string) {
		if v, err := r.p.label(value, valueOff); err == nil {
			*into = v
		}
	}
	element := n.Type == ReqElement
	switch {
	case !element && key == "id":
		plain(&n.ID)
	case !element && key == "text":
		plain(&n.Text)
	case !element && key == "risk":
		for k, name := range reqRiskNames {
			if name != "" && strings.EqualFold(name, value) {
				n.Risk = RequirementRisk(k)
				return
			}
		}
		r.p.fail(valueOff, "risk is low, medium or high, not %q", value)
	case !element && key == "verifymethod":
		for k, name := range reqVerifyNames {
			if name != "" && strings.EqualFold(name, value) {
				n.Verify = RequirementVerify(k)
				return
			}
		}
		r.p.fail(valueOff, "verifymethod is analysis, inspection, test or demonstration, not %q", value)
	case element && key == "type":
		plain(&n.ElemType)
	case element && key == "docref":
		plain(&n.DocRef)
	default:
		what := "a requirement"
		if element {
			what = "an element"
		}
		r.p.fail(off, "%s has no field %q", what, key)
	}
}

// relations reads each relationship, now every node is defined: both ends must name one.
func (r *reqParser) relations() {
next:
	for _, st := range r.rels {
		var src, kind, dst string
		if m := reqBackward.FindStringSubmatch(st.text); m != nil {
			dst, kind, src = m[1], m[2], m[3]
		} else {
			m := reqForward.FindStringSubmatch(st.text)
			src, kind, dst = m[1], m[2], m[3]
		}
		k := RequirementRelKind(0)
		for i, n := range reqRelNames {
			if n != "" && strings.EqualFold(n, kind) {
				k = RequirementRelKind(i)
			}
		}
		if k == 0 {
			r.p.fail(st.off, "no relationship is called %q", kind)
			continue
		}
		var ends [2]string
		for i, s := range []string{src, dst} {
			n, ok := r.name(s, st.off)
			if !ok {
				continue next
			}
			if _, defined := r.index[n]; !defined {
				r.p.fail(st.off, "%q names no requirement or element", n)
				continue next
			}
			ends[i] = n
		}
		r.d.Relations = append(r.d.Relations, RequirementRelation{Src: ends[0], Dst: ends[1], Kind: k,
			Span: [2]int{st.off, st.off + len(st.text)}})
	}
}

// resolve gives each node its classes and resolved style.
func (r *reqParser) resolve() {
	for _, cu := range r.classOf {
		if i, ok := r.index[cu.id]; ok {
			r.d.Nodes[i].Classes = append(r.d.Nodes[i].Classes, cu.class)
		}
	}
	for i := range r.d.Nodes {
		n := &r.d.Nodes[i]
		st := slices.Clone(r.d.Classes["default"])
		for _, c := range n.Classes {
			st = merge(st, r.d.Classes[c])
		}
		st = merge(st, r.styles[n.Name])
		if len(st) > 0 {
			n.Style = st
		}
	}
}
