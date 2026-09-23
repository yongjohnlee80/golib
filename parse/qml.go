package parse

import (
	"fmt"
	"strings"
	"unicode"
)

// QML parses a declarative UI schema into a [SpecTree].
//
// It is a SYNTAX parser and nothing more. It knows what a node, a property and
// a handler look like; it does not know that Button has a label, that a colour
// belongs in a token-only property, or that any type exists at all. Property
// existence and typing live in the UI adapter's widget registry, and this
// package cannot see that registry without importing the adapter — which would
// invert the dependency and make one text format answerable to one UI toolkit.
//
// That boundary is why [Value] carries a Kind and a Position rather than a
// resolved Go value: the registry validates and converts later, and reports
// with the Position recorded here (D8). The one rule this costs us is worth
// stating plainly — the parser CANNOT reject `"#1e1e2e"`, because
// `Text.text` may legitimately contain exactly that string and only the
// registry knows the difference.
//
// # The grammar, and why it is this small
//
//	Root     := Node
//	Node     := TypeName '{' Body '}'
//	Body     := ( Property | SpecHandler | Node )*
//	Property := Ident ':' Value
//	SpecHandler  := 'on' Ident ':' Ident        // a handler NAME, never a body
//	Value    := String | Number | Bool | Token | Ref | Call
//	Token    := '@' Ident                   // a portable style token
//	Ref      := Ident
//	Call     := Ident '(' [ Value { ',' Value } ] ')'
//
// There is no arithmetic and there are no member chains. `width: parent.width / 2`
// is where an expression evaluator starts, and an expression evaluator is where
// an ECMAScript runtime ends. Growing the grammar should take a specific screen
// that needs it, not a general appetite for expressiveness.
//
// SpecHandler bodies are NAMES for the same reason: the parser emits data, never
// behaviour, so one schema file is meaningful to any adapter that can resolve
// the names (D2 rule 2).
//
// The zero value is usable.
type QML struct {
	// MaxDepth bounds node nesting. Zero means [DefaultQMLMaxDepth].
	//
	// A schema is usually hand-written and shallow, but this parser is
	// recursive and a reload path may be handed a file that a watcher caught
	// mid-write or that arrived from somewhere less trusted. A depth bound
	// turns a stack overflow — which takes the process down and cannot be
	// recovered — into an ordinary [SyntaxError] the caller can show.
	MaxDepth int
}

// DefaultQMLMaxDepth is the nesting limit QML applies when MaxDepth is zero.
// Deep enough that no hand-written screen will reach it, shallow enough that
// the recursion cannot exhaust a goroutine stack.
const DefaultQMLMaxDepth = 64

// FormatName implements [Named].
func (QML) FormatName() string { return "qml" }

// SpecValueKind classifies a property value LEXICALLY — by how it was written, not
// by what it means. A registry descriptor decides whether a given kind is
// acceptable for a given property is the registry's judgement, made where the
// property's type is known.
type SpecValueKind uint8

const (
	// SpecValueInvalid is the zero value and never appears in a parsed tree.
	SpecValueInvalid SpecValueKind = iota
	// SpecValueString is a quoted string. Raw holds the unquoted contents.
	SpecValueString
	// SpecValueNumber is a numeric literal. Raw holds it as written, undecoded:
	// the registry knows whether the target is an int, a float or a ratio,
	// and decoding here would pick one of those answers too early.
	SpecValueNumber
	// SpecValueBool is true or false.
	SpecValueBool
	// SpecValueToken is a style token reference written @name.
	//
	// Raw holds the bare name ("surface"), a PORTABLE SYMBOLIC VALUE rather
	// than a tui/style.Token — otherwise the same schema stops meaning
	// anything to a non-terminal adapter, which is the point of the layering.
	SpecValueToken
	// SpecValueRef is a bare identifier: a single reference, resolved by the
	// adapter. Not a member chain — see the grammar note on the type.
	SpecValueRef
	// SpecValueCall is a call into the host function registry. Raw holds the
	// function name and Args holds the arguments, which are themselves Values.
	SpecValueCall
)

// String renders the kind for diagnostics.
func (k SpecValueKind) String() string {
	switch k {
	case SpecValueString:
		return "string"
	case SpecValueNumber:
		return "number"
	case SpecValueBool:
		return "bool"
	case SpecValueToken:
		return "token"
	case SpecValueRef:
		return "reference"
	case SpecValueCall:
		return "call"
	default:
		return "invalid"
	}
}

// Value is one property value, classified but not interpreted.
//
// Pos is carried on every value because it is what a registry-level type error
// reports with: the parser judged the syntax, the registry judges the meaning,
// and the person who wrote the file needs the error to point at their line
// either way.
type SpecValue struct {
	Kind SpecValueKind
	// Raw is the value as written, with string quotes removed and the token
	// sigil stripped. For SpecValueCall it is the function name.
	Raw string
	// Args are the arguments of a SpecValueCall, empty otherwise.
	Args []SpecValue
	Pos  Position
}

// SpecProp is one `name: value` pair.
type SpecProp struct {
	Name  string
	Value SpecValue
	Pos   Position
}

// SpecHandler is one `onSignal: handlerName` binding.
//
// Signal is the signal name with the `on` prefix removed and the first letter
// lowercased, so `onClicked` becomes "clicked" — the name the adapter registers
// slots under. Name is the host function the adapter resolves.
type SpecHandler struct {
	Signal string
	Name   string
	Pos    Position
}

// SpecNode is one node of the schema tree: pure data, no behaviour.
type SpecNode struct {
	// Type is the declared type name, matched against the adapter's registry.
	Type string
	// ID is the declared `id:` if the node has one, empty otherwise.
	//
	// This is the identity a reloading consumer matches old tree to new by.
	// When present it is authoritative: it is what the author wrote and what
	// diagnostics name, so an identity derived from the built component must
	// not silently disagree with it. It is lifted out of Props because it
	// addresses the node rather than configuring it — nothing sets an `id` on
	// a widget.
	ID string
	// Props are in DOCUMENT ORDER, which is the order they are applied and,
	// for handlers, the order they run. Preserving it is not a convenience: a
	// map would make application order an implementation detail, when it
	// should be readable straight off the file.
	Props    []SpecProp
	Handlers []SpecHandler
	Children []*SpecNode
	Pos      Position
}

// SpecTree is a parsed schema.
type SpecTree struct {
	Root *SpecNode
}

// Parse implements [Parser]. It returns a [SyntaxError] on malformed input.
//
// When the source ends in the middle of a construct the error carries
// Incomplete, and it points at where the construct OPENED rather than at the
// end of the file — the unclosed brace is what the writer needs to find. That
// distinction is what lets a reload path tell "still being written" from
// "wrong" and hold the last good tree instead of flashing an error on every
// save.
func (q QML) Parse(src []byte) (SpecTree, error) {
	sc := NewScanner(src)
	p := &qmlParser{sc: sc, maxDepth: q.MaxDepth}
	if p.maxDepth <= 0 {
		p.maxDepth = DefaultQMLMaxDepth
	}

	if err := p.skipSpace(); err != nil {
		return SpecTree{}, err
	}
	if p.sc.Done() {
		return SpecTree{}, SyntaxError{
			Format: "qml", Pos: p.sc.Pos(),
			Want: "a root node", Got: "end of input", Incomplete: true,
		}
	}

	root, err := p.node()
	if err != nil {
		return SpecTree{}, err
	}

	if err := p.skipSpace(); err != nil {
		return SpecTree{}, err
	}
	if !p.sc.Done() {
		r, _ := p.sc.Peek()
		return SpecTree{}, SyntaxError{
			Format: "qml", Pos: p.sc.Pos(),
			Want: "end of input after the root node",
			Got:  quoteRune(r),
		}
	}
	return SpecTree{Root: root}, nil
}

// qmlParser holds the scan state for one Parse call. A parser value is never
// reused, so QML itself stays immutable and safe to share.
type qmlParser struct {
	sc       *Scanner
	maxDepth int
	// depth counts EVERY live recursive frame, node and value alike.
	//
	// The first cut budgeted node nesting only, and a call argument list
	// recurses through value just as deeply: f(f(f(...))) five thousand deep
	// parsed happily. The stack does not care which function recursed, so
	// neither does the budget.
	depth int
}

// enter takes one unit of recursion budget, or reports why it cannot.
func (p *qmlParser) enter(at Position) error {
	if p.depth >= p.maxDepth {
		return SyntaxError{
			Format: "qml", Pos: at,
			Want: fmt.Sprintf("nesting no deeper than %d", p.maxDepth),
			Got:  "deeper nesting",
		}
	}
	p.depth++
	return nil
}

func (p *qmlParser) leave() { p.depth-- }

// node parses `TypeName { ... }`.
func (p *qmlParser) node() (*SpecNode, error) {
	startPos := p.sc.Pos()
	name, ok := p.ident()
	if !ok {
		r, ok := p.sc.Peek()
		if !ok {
			return nil, SyntaxError{
				Format: "qml", Pos: startPos,
				Want: "a type name", Got: "end of input", Incomplete: true,
			}
		}
		return nil, SyntaxError{
			Format: "qml", Pos: startPos,
			Want: "a type name", Got: quoteRune(r),
		}
	}
	return p.nodeBody(name, startPos)
}

// nodeBody parses `{ ... }` for a type name the caller has already read.
//
// It exists so the member loop can commit to a child node using the identifier
// it already consumed. The alternative — rewinding the scanner — would need
// arbitrary lookahead, and Scanner.Unread is deliberately one token deep.
func (p *qmlParser) nodeBody(name string, startPos Position) (*SpecNode, error) {
	if err := p.enter(startPos); err != nil {
		return nil, err
	}
	defer p.leave()

	if !isTypeName(name) {
		return nil, SyntaxError{
			Format: "qml", Pos: startPos,
			Want: "a type name starting with an upper-case letter",
			Got:  quoted(name),
		}
	}

	n := &SpecNode{Type: name, Pos: startPos}

	if err := p.skipSpace(); err != nil {
		return nil, err
	}
	braceAt := p.sc.Pos()
	if !p.sc.Take("{") {
		r, ok := p.sc.Peek()
		if !ok {
			return nil, SyntaxError{
				Format: "qml", Pos: braceAt,
				Want: "{ to open " + name, Got: "end of input", Incomplete: true,
			}
		}
		return nil, SyntaxError{
			Format: "qml", Pos: braceAt,
			Want: "{ to open " + name, Got: quoteRune(r),
		}
	}

	for {
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		if p.sc.Done() {
			return nil, SyntaxError{
				Format: "qml", Pos: braceAt,
				Want: "} to close " + name + " opened here",
				Got:  "end of input", Incomplete: true,
			}
		}
		if p.sc.Take("}") {
			return n, nil
		}

		memberAt := p.sc.Pos()
		name, ok := p.ident()
		if !ok {
			r, _ := p.sc.Peek()
			return nil, SyntaxError{
				Format: "qml", Pos: memberAt,
				Want: "a property, a handler, a child node, or }",
				Got:  quoteRune(r),
			}
		}

		// A child node is an identifier followed by `{`; a property or handler
		// is an identifier followed by `:`. One rune of lookahead separates
		// them, which is the whole reason the grammar spells a child as a
		// TypeName rather than something that needs backtracking.
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		switch {
		case p.sc.HasPrefix("{"):
			child, err := p.nodeBody(name, memberAt)
			if err != nil {
				return nil, err
			}
			n.Children = append(n.Children, child)

		case p.sc.Take(":"):
			if err := p.member(n, name, memberAt); err != nil {
				return nil, err
			}

		default:
			r, ok := p.sc.Peek()
			if !ok {
				return nil, SyntaxError{
					Format: "qml", Pos: memberAt,
					Want: ": or { after " + quoted(name), Got: "end of input",
					Incomplete: true,
				}
			}
			return nil, SyntaxError{
				Format: "qml", Pos: memberAt,
				Want: ": or { after " + quoted(name), Got: quoteRune(r),
			}
		}
	}
}

// member parses the right-hand side of `name:` — a handler when the name is an
// on-prefixed signal, a property otherwise.
func (p *qmlParser) member(n *SpecNode, name string, at Position) error {
	if err := p.skipSpace(); err != nil {
		return err
	}

	if sig, ok := signalName(name); ok {
		hAt := p.sc.Pos()
		target, ok := p.ident()
		if !ok {
			r, ok := p.sc.Peek()
			if !ok {
				return SyntaxError{
					Format: "qml", Pos: hAt,
					Want:       "a handler name for " + quoted(name),
					Got:        "end of input",
					Incomplete: true,
				}
			}
			// The most likely mistake here is writing a body, so the error
			// says what this format wants instead of just what it found.
			return SyntaxError{
				Format: "qml", Pos: hAt,
				Want: "a handler NAME for " + quoted(name) +
					" (this format binds handlers by name; it has no expression syntax)",
				Got: quoteRune(r),
			}
		}
		n.Handlers = append(n.Handlers, SpecHandler{Signal: sig, Name: target, Pos: at})
		return nil
	}

	v, err := p.value()
	if err != nil {
		return err
	}

	// `id` addresses the node rather than configuring it, so it is lifted out
	// of Props. It must be a bare identifier: an id that came from a call or a
	// binding could change between reloads, and an identity that moves is not
	// an identity.
	if name == "id" {
		if v.Kind != SpecValueRef {
			return SyntaxError{
				Format: "qml", Pos: v.Pos,
				Want: "a bare identifier for id",
				Got:  v.Kind.String(),
			}
		}
		if n.ID != "" {
			return SyntaxError{
				Format: "qml", Pos: at,
				Want: "at most one id per node",
				Got:  "a second id",
			}
		}
		n.ID = v.Raw
		return nil
	}

	n.Props = append(n.Props, SpecProp{Name: name, Value: v, Pos: at})
	return nil
}

// value parses one property value.
func (p *qmlParser) value() (SpecValue, error) {
	if err := p.skipSpace(); err != nil {
		return SpecValue{}, err
	}
	at := p.sc.Pos()

	r, ok := p.sc.Peek()
	if !ok {
		return SpecValue{}, SyntaxError{
			Format: "qml", Pos: at,
			Want: "a value", Got: "end of input", Incomplete: true,
		}
	}

	switch {
	case r == '"':
		return p.stringValue()

	case r == '@':
		p.sc.Next()
		name, ok := p.ident()
		if !ok {
			r2, ok := p.sc.Peek()
			if !ok {
				return SpecValue{}, SyntaxError{
					Format: "qml", Pos: at,
					Want: "a token name after @", Got: "end of input", Incomplete: true,
				}
			}
			return SpecValue{}, SyntaxError{
				Format: "qml", Pos: at,
				Want: "a token name after @", Got: quoteRune(r2),
			}
		}
		return SpecValue{Kind: SpecValueToken, Raw: name, Pos: at}, nil

	case r == '-' || r == '+' || (r >= '0' && r <= '9'):
		return p.numberValue()

	case isIdentStart(r):
		name, _ := p.ident()
		switch name {
		case "true", "false":
			return SpecValue{Kind: SpecValueBool, Raw: name, Pos: at}, nil
		}
		// A call is an identifier followed by `(`. No space is permitted
		// between them, so `foo ()` is a reference followed by a syntax error
		// rather than a call — one shape per meaning.
		if p.sc.HasPrefix("(") {
			return p.callValue(name, at)
		}
		return SpecValue{Kind: SpecValueRef, Raw: name, Pos: at}, nil
	}

	return SpecValue{}, SyntaxError{
		Format: "qml", Pos: at,
		Want: "a value", Got: quoteRune(r),
	}
}

func (p *qmlParser) callValue(name string, at Position) (SpecValue, error) {
	if err := p.enter(at); err != nil {
		return SpecValue{}, err
	}
	defer p.leave()

	openAt := p.sc.Pos()
	p.sc.Take("(")
	v := SpecValue{Kind: SpecValueCall, Raw: name, Pos: at}

	if err := p.skipSpace(); err != nil {
		return SpecValue{}, err
	}
	if p.sc.Take(")") {
		return v, nil
	}
	for {
		arg, err := p.value()
		if err != nil {
			return SpecValue{}, err
		}
		v.Args = append(v.Args, arg)

		if err := p.skipSpace(); err != nil {
			return SpecValue{}, err
		}
		if p.sc.Done() {
			return SpecValue{}, SyntaxError{
				Format: "qml", Pos: openAt,
				Want: ") to close the call opened here", Got: "end of input",
				Incomplete: true,
			}
		}
		if p.sc.Take(")") {
			return v, nil
		}
		if p.sc.Take(",") {
			continue
		}
		r, _ := p.sc.Peek()
		return SpecValue{}, SyntaxError{
			Format: "qml", Pos: p.sc.Pos(),
			Want: ", or ) in the argument list", Got: quoteRune(r),
		}
	}
}

func (p *qmlParser) stringValue() (SpecValue, error) {
	at := p.sc.Pos()
	p.sc.Next() // opening quote

	var b strings.Builder
	for {
		r, ok := p.sc.Next()
		if !ok {
			return SpecValue{}, SyntaxError{
				Format: "qml", Pos: at,
				Want: `a closing " for the string opened here`, Got: "end of input",
				Incomplete: true,
			}
		}
		switch r {
		case '"':
			return SpecValue{Kind: SpecValueString, Raw: b.String(), Pos: at}, nil
		case '\\':
			esc, ok := p.sc.Next()
			if !ok {
				return SpecValue{}, SyntaxError{
					Format: "qml", Pos: at,
					Want: `a closing " for the string opened here`, Got: "end of input",
					Incomplete: true,
				}
			}
			switch esc {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			case '"', '\\':
				b.WriteRune(esc)
			default:
				return SpecValue{}, SyntaxError{
					Format: "qml", Pos: p.sc.Pos(),
					Want: `an escape of \n, \t, \" or \\`,
					Got:  quoteRune(esc),
				}
			}
		case '\n':
			// A newline inside a string is nearly always a missing quote, and
			// reporting it at the opening quote points at the actual mistake.
			return SpecValue{}, SyntaxError{
				Format: "qml", Pos: at,
				Want: `a closing " before the end of the line`,
				Got:  "a newline",
			}
		default:
			b.WriteRune(r)
		}
	}
}

func (p *qmlParser) numberValue() (SpecValue, error) {
	at := p.sc.Pos()
	var b strings.Builder

	if r, _ := p.sc.Peek(); r == '-' || r == '+' {
		r, _ = p.sc.Next()
		b.WriteRune(r)
	}
	digits := 0
	dots := 0
	for {
		r, ok := p.sc.Peek()
		if !ok {
			break
		}
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.':
			dots++
			if dots > 1 {
				return SpecValue{}, SyntaxError{
					Format: "qml", Pos: p.sc.Pos(),
					Want: "at most one decimal point", Got: quoteRune(r),
				}
			}
		default:
			goto done
		}
		p.sc.Next()
		b.WriteRune(r)
	}
done:
	if digits == 0 {
		return SpecValue{}, SyntaxError{
			Format: "qml", Pos: at,
			Want: "digits in the number", Got: quoted(b.String()),
		}
	}
	// Raw is left undecoded on purpose — see SpecValueNumber.
	return SpecValue{Kind: SpecValueNumber, Raw: b.String(), Pos: at}, nil
}

// ident reads an identifier, or reports false without consuming anything.
func (p *qmlParser) ident() (string, bool) {
	r, ok := p.sc.Peek()
	if !ok || !isIdentStart(r) {
		return "", false
	}
	var b strings.Builder
	for {
		r, ok := p.sc.Peek()
		if !ok || !isIdentPart(r) {
			break
		}
		p.sc.Next()
		b.WriteRune(r)
	}
	return b.String(), true
}

// skipSpace consumes whitespace and comments, and REPORTS an unterminated block
// comment rather than swallowing it.
//
// Comments are skipped rather than recorded: nothing downstream consumes them,
// and a tree that carried them would make every diff answer for text that
// cannot affect what is rendered.
//
// Returning an error here matters more than it looks. The first cut let a block
// comment run to end of input and left the complaint to "whatever expected a
// token next" — but after a complete root node nothing expects a token, so
// `N { } /* never closed` parsed as a SUCCESS. An unterminated construct is
// exactly the thing a reload path must hear about, because it is what a
// half-written file looks like.
func (p *qmlParser) skipSpace() error {
	for {
		for {
			r, ok := p.sc.Peek()
			if !ok || !unicode.IsSpace(r) {
				break
			}
			p.sc.Next()
		}
		switch {
		case p.sc.HasPrefix("//"):
			for {
				r, ok := p.sc.Next()
				if !ok || r == '\n' {
					break
				}
			}
		case p.sc.HasPrefix("/*"):
			openedAt := p.sc.Pos()
			p.sc.Take("/*")
			for {
				if p.sc.Done() {
					return SyntaxError{
						Format: "qml", Pos: openedAt,
						Want:       "*/ to close the comment opened here",
						Got:        "end of input",
						Incomplete: true,
					}
				}
				if p.sc.Take("*/") {
					break
				}
				p.sc.Next()
			}
		default:
			return nil
		}
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isTypeName reports whether name looks like a node type rather than a
// property. Upper-case initial is the whole rule, and it is what lets a reader
// tell a child node from a property at a glance — the same convention QML and
// Go both use.
func isTypeName(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsUpper([]rune(name)[0])
}

// signalName reports the signal for an `onX` member name, and false when the
// name is an ordinary property. `on` alone is a property, not a signal with an
// empty name.
func signalName(name string) (string, bool) {
	if !strings.HasPrefix(name, "on") || len(name) < 3 {
		return "", false
	}
	rest := []rune(name[2:])
	if !unicode.IsUpper(rest[0]) {
		return "", false
	}
	rest[0] = unicode.ToLower(rest[0])
	return string(rest), true
}

func quoteRune(r rune) string { return `"` + string(r) + `"` }

func quoted(s string) string { return `"` + s + `"` }
