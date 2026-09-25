package qml

import (
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/js"

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
// That boundary is why [SpecValue] carries a Kind and a parse.Position rather than a
// resolved Go value: the registry validates and converts later, and reports
// with the parse.Position recorded here. The one rule this costs us is worth
// stating plainly — the parser CANNOT reject `"#1e1e2e"`, because
// `Text.text` may legitimately contain exactly that string and only the
// registry knows the difference.
//
// # The grammar
//
//	Root     := { Import } Node
//	Import   := 'import' DottedName [ Version ] [ 'as' Qualifier ]
//	Node     := TypeName '{' Body '}'
//	Body     := ( Property | Handler | Group | Node )*
//	Property := PropName ':' Value
//	Group    := PropName '{' ( Property )* '}'    // font { bold: true }
//	PropName := Ident { '.' Ident }               // plain, grouped, or attached
//	Handler  := 'on' Ident ':' JavaScript         // one statement, or a block
//	Value    := JavaScript                        // an expression
//
// A PROPERTY VALUE IS A JAVASCRIPT EXPRESSION and a HANDLER BODY IS JAVASCRIPT
// STATEMENTS, because in QML that is what they are. Both are read by
// [github.com/yongjohnlee80/golib/parse/js], on this parser's own scanner and
// depth budget, so nesting is bounded across the two grammars rather than
// within each.
//
// [SpecValue] PROJECTS the shapes a consumer asks about constantly — string,
// number, bool, ref, call — and keeps the tree for everything else as
// [SpecValueExpr]. That is a convenience, not a smaller grammar: `parent.width
// / 2` parses, and an engine that cannot evaluate it declines it by name and
// position rather than this package pretending the syntax is wrong.
//
// There are no extensions. There was briefly one — `@name`, a symbolic style
// value — and it is gone: QML defines styles with a singleton object reached
// through an import, so the sigil was a second way to spell something the
// language already had.
//
// A qualifier after `as` is written like a type, because it names a namespace,
// and an import statement ends at the end of its line.
//
// The zero value is usable.
type QML struct {
	// MaxDepth bounds node nesting. Zero means [DefaultQMLMaxDepth].
	//
	// A schema is usually hand-written and shallow, but this parser is
	// recursive and a reload path may be handed a file that a watcher caught
	// mid-write or that arrived from somewhere less trusted. A depth bound
	// turns a stack overflow — which takes the process down and cannot be
	// recovered — into an ordinary [parse.SyntaxError] the caller can show.
	MaxDepth int
	// File names the source for diagnostics, and is carried by every position
	// in the tree: "QuitDialog.qml:3:5" rather than a bare 3:5 once a screen
	// is made of several files.
	File string
}

// DefaultQMLMaxDepth is the nesting limit QML applies when MaxDepth is zero.
// Deep enough that no hand-written screen will reach it, shallow enough that
// the recursion cannot exhaust a goroutine stack.
const DefaultQMLMaxDepth = 64

// FormatName implements [parse.Named].
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
	// SpecValueRef is a name written bare: `greeting`, or a member chain like
	// `parent.width`.
	//
	// Raw holds it AS WRITTEN, dots and all, and [SpecValue.Path] holds the
	// segments. A single-segment reference therefore reads exactly as it always
	// did — Raw is the whole name — so a consumer that never expected a chain
	// keeps working unchanged.
	//
	// Who resolves it is the consumer's business: a UI engine may read a
	// host-provided value of that name, and an adapter may read its own
	// vocabulary. This package only records what was written.
	SpecValueRef
	// SpecValueCall is a call into the host function registry. Raw holds the
	// function name and Args holds the arguments, which are themselves Values.
	SpecValueCall
	// SpecValueExpr is any other JavaScript expression — `a + b`, `c ? d : e`,
	// `items[i]`, a template string. Expr holds the parsed tree.
	//
	// It exists because QML property values ARE JavaScript, and a parser of QML
	// reads all of it. The kinds above are not a different grammar: they are the
	// shapes this format projects to a friendlier terminal because consumers ask
	// about them constantly. Everything else keeps its tree rather than being
	// refused, so a consumer that cannot evaluate an expression declines it by
	// name and position instead of the parser pretending the syntax is invalid.
	SpecValueExpr
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
	case SpecValueRef:
		return "reference"
	case SpecValueCall:
		return "call"
	case SpecValueExpr:
		return "expression"
	default:
		return "invalid"
	}
}

// SpecValue is one property value, classified but not interpreted.
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
	// Path holds the segments of a SpecValueRef: ["parent", "width"] for
	// `parent.width`, and ["greeting"] for a plain name. Empty for every other
	// kind.
	Path []string
	// Expr is the parsed expression of a SpecValueExpr, nil for every other
	// kind. The projected kinds do NOT also carry it: one representation per
	// value, so nothing downstream can read two answers to the same question
	// and no projection can drift from the tree it came from.
	Expr *js.Expr
	Pos  parse.Position
}

// SpecProp is one `name: value` pair.
type SpecProp struct {
	// Name is the property as written, dots included: "text", "font.bold",
	// "Layout.fillWidth". A consumer that only ever expected a plain name reads
	// this exactly as it did before.
	Name string
	// Path holds the segments. One for a plain property; more for a GROUPED
	// property (`font.bold`) or an ATTACHED one (`Layout.fillWidth`).
	//
	// QML distinguishes those two by convention — an attached property's first
	// segment is capitalised, because it names a type rather than a
	// sub-object — and this package records the spelling without ruling on it.
	// Which of the two a name means depends on what the consumer has
	// registered, and that is not something a parser can know.
	Path []string
	// Grouped reports that this property was written inside a block —
	// `font { bold: true }` rather than `font.bold: true`. The two MEAN the
	// same thing and carry the same Path; this records which was written, for a
	// consumer that formats or round-trips and would otherwise rewrite one into
	// the other.
	Grouped bool
	Value   SpecValue
	Pos     parse.Position
}

// SpecImport is one `import` statement.
//
// QML uses imports to bring a module's types into scope. This package records
// what was imported; resolving it — deciding which types a module provides, or
// that it does not exist — belongs to whoever consumes the tree.
type SpecImport struct {
	// Module is the dotted name as written: "tui.Window", "QtQuick".
	Module string
	// Path holds the module name's segments.
	Path []string
	// Version is the version as written, empty when none was given. It is left
	// UNDECODED for the same reason numbers are: a consumer with a version
	// policy knows how to read it, and this package would be guessing.
	Version string
	// Alias is the name after `as`, empty when there is none.
	Alias string
	Pos   parse.Position
}

// SpecHandler is one `onSignal:` binding and the JavaScript it binds.
//
// Signal is the signal name with the `on` prefix removed and the first letter
// lowercased, so `onClicked` becomes "clicked" — the name the adapter registers
// slots under.
type SpecHandler struct {
	Signal string
	// Body is the handler's statements, in order.
	//
	// It is ONE AST for both QML spellings: `onClicked: save()` is a single
	// expression statement and `onClicked: { … }` is that block's contents, so
	// nothing downstream has to ask which form was written. There is
	// deliberately no separate "handler name" field — `onClicked: save` is an
	// identifier expression and NOT a call to save(), and a field that flattened
	// it to the name "save" would erase that distinction inside the parser,
	// where nothing can see it any more.
	Body []js.Stmt
	// Path holds the segments of an ATTACHED handler —
	// `Component.onCompleted` — and one segment for an ordinary one. As with
	// [SpecProp], the parser records the spelling and does not rule on what it
	// attaches to.
	Path []string
	Pos  parse.Position
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
	Pos      parse.Position
}

// SpecTree is a parsed schema.
type SpecTree struct {
	// Imports are in document order, before the root node.
	Imports []SpecImport
	Root    *SpecNode
}

// Parse implements [Parser]. It returns a [parse.SyntaxError] on malformed input.
//
// When the source ends in the middle of a construct the error carries
// Incomplete, and it points at where the construct OPENED rather than at the
// end of the file — the unclosed brace is what the writer needs to find. That
// distinction is what lets a reload path tell "still being written" from
// "wrong" and hold the last good tree instead of flashing an error on every
// save.
func (q QML) Parse(src []byte) (SpecTree, error) {
	sc := parse.NewFileScanner(q.File, src)
	p := &qmlParser{sc: sc, maxDepth: q.MaxDepth}
	if p.maxDepth <= 0 {
		p.maxDepth = DefaultQMLMaxDepth
	}

	if err := p.skipSpace(); err != nil {
		return SpecTree{}, err
	}
	if p.sc.Done() {
		return SpecTree{}, parse.SyntaxError{
			Format: "qml", Pos: p.sc.Pos(),
			Want: "a root node", Got: "end of input", Incomplete: true,
		}
	}

	var imports []SpecImport
	for {
		if err := p.skipSpace(); err != nil {
			return SpecTree{}, err
		}
		if !p.sc.HasPrefix("import") {
			break
		}
		// `importer { }` is a node, not an import: the keyword only counts when
		// a non-identifier character follows it.
		if r, ok := p.sc.PeekAt(len("import")); ok && isIdentPart(r) {
			break
		}
		imp, err := p.importStatement()
		if err != nil {
			return SpecTree{}, err
		}
		imports = append(imports, imp)
	}

	if err := p.skipSpace(); err != nil {
		return SpecTree{}, err
	}
	if p.sc.Done() {
		return SpecTree{}, parse.SyntaxError{
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
		return SpecTree{}, parse.SyntaxError{
			Format: "qml", Pos: p.sc.Pos(),
			Want: "end of input after the root node",
			Got:  parse.QuoteRune(r),
		}
	}
	return SpecTree{Imports: imports, Root: root}, nil
}

// qmlParser holds the scan state for one Parse call. A parser value is never
// reused, so QML itself stays immutable and safe to share.
type qmlParser struct {
	sc       *parse.Scanner
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
func (p *qmlParser) enter(at parse.Position) error {
	if p.depth >= p.maxDepth {
		return parse.SyntaxError{
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
// importStatement reads `import a.b.c 1.0 as Name`.
//
// Version and alias are optional and are recorded as written. A parser that
// validated either would be deciding a policy — which versions exist, which
// aliases collide — that belongs to whatever resolves the module.
func (p *qmlParser) importStatement() (SpecImport, error) {
	at := p.sc.Pos()
	p.sc.Take("import")

	if err := p.skipSpace(); err != nil {
		return SpecImport{}, err
	}
	name, ok := p.ident()
	if !ok {
		return SpecImport{}, p.wanted(at, "a module name after import")
	}
	imp := SpecImport{Module: name, Path: []string{name}, Pos: at}
	for p.sc.HasPrefix(".") {
		dotAt := p.sc.Pos()
		p.sc.Take(".")
		seg, ok := p.ident()
		if !ok {
			return SpecImport{}, p.wanted(dotAt, "a name after . in the module name")
		}
		imp.Path = append(imp.Path, seg)
		imp.Module += "." + seg
	}

	// A version is digits and dots, and it is optional. It must not swallow the
	// next line's node, so it is only read when a digit follows on this line.
	if r, ok := p.sc.Peek(); ok && r == ' ' || r == '\t' {
		for {
			r, ok := p.sc.Peek()
			if !ok || (r != ' ' && r != '\t') {
				break
			}
			p.sc.Next()
		}
		if r, ok := p.sc.Peek(); ok && r >= '0' && r <= '9' {
			start := p.sc.Pos().Offset
			for {
				r, ok := p.sc.Peek()
				if !ok || !(r >= '0' && r <= '9' || r == '.') {
					break
				}
				p.sc.Next()
			}
			imp.Version = string(p.sc.Slice(start, p.sc.Pos().Offset))
		}
	}

	if err := p.skipSpace(); err != nil {
		return SpecImport{}, err
	}
	if p.sc.HasPrefix("as") {
		if r, ok := p.sc.PeekAt(2); !ok || !isIdentPart(r) {
			asAt := p.sc.Pos()
			p.sc.Take("as")
			// The qualifier is on the SAME LINE. An import statement ends at
			// the end of its line, so skipping arbitrary whitespace here read
			// the next line's first word as the alias: `import tui 1.0 as`
			// followed by `Window { }` bound the qualifier "Window" and then
			// complained that the document had no root.
			p.skipInlineSpace()
			alias, ok := p.ident()
			if !ok {
				return SpecImport{}, p.wanted(asAt, "a name after as")
			}
			// QML's qualifier names a namespace, and a namespace is written
			// like a type. A lower-case one is refused HERE rather than
			// resolving to nothing later, when the mistake is three screens
			// away from the import that caused it.
			if !isTypeName(alias) {
				return SpecImport{}, parse.SyntaxError{
					Format: "qml", Pos: asAt,
					Want: "an upper-case qualifier after as, because it names a namespace",
					Got:  parse.Quoted(alias),
				}
			}
			imp.Alias = alias
		}
	}
	return imp, nil
}

// groupedBlock reads `font { bold: true  size: 14 }`.
//
// Each entry becomes a SpecProp whose Path is the block's prefix followed by
// the entry's own, so `font { bold: true }` and `font.bold: true` produce the
// same Path — they mean the same thing — while [SpecProp.Grouped] records which
// spelling was used, for a consumer that formats or round-trips.
func (p *qmlParser) groupedBlock(n *SpecNode, prefix []string, at parse.Position) error {
	if err := p.enter(at); err != nil {
		return err
	}
	defer p.leave()

	p.sc.Take("{")
	for {
		if err := p.skipSpace(); err != nil {
			return err
		}
		if p.sc.Done() {
			return parse.SyntaxError{
				Format: "qml", Pos: at,
				Want: "} to close the group opened here", Got: "end of input",
				Incomplete: true,
			}
		}
		if p.sc.Take("}") {
			return nil
		}
		entryAt := p.sc.Pos()
		leaf, ok := p.ident()
		if !ok {
			r, _ := p.sc.Peek()
			return parse.SyntaxError{
				Format: "qml", Pos: entryAt,
				Want: "a property name or } in the group", Got: parse.QuoteRune(r),
			}
		}
		full := append(append([]string{}, prefix...), leaf)
		for p.sc.HasPrefix(".") {
			dotAt := p.sc.Pos()
			p.sc.Take(".")
			seg, ok := p.ident()
			if !ok {
				return p.wanted(dotAt, "a name after . in the property name")
			}
			full = append(full, seg)
		}
		if err := p.skipSpace(); err != nil {
			return err
		}
		// `font { style { weight: 700 } }` — a group inside a group. The prefix
		// accumulates, so the leaf path is the same one the dotted spelling
		// would produce.
		if p.sc.HasPrefix("{") {
			if err := p.groupedBlock(n, full, entryAt); err != nil {
				return err
			}
			continue
		}
		if !p.sc.Take(":") {
			return p.wanted(entryAt, ": or { after the property name in the group")
		}
		v, err := p.value()
		if err != nil {
			return err
		}
		p.takeStatementEnd()
		n.Props = append(n.Props, SpecProp{
			Name: strings.Join(full, "."), Path: full, Value: v, Grouped: true, Pos: entryAt,
		})
	}
}

// wanted reports a failure, distinguishing end of input — which a writer has
// simply not finished — from a wrong character.
// takeStatementEnd consumes the `;` that may end a property binding.
//
// A property's right-hand side is a JavaScript statement, and a statement may
// end with a semicolon — which is why `Rectangle { width: 100; height: 100 }`
// is ordinary QML. The `;` belongs to the binding it follows; it is NOT a
// separator the member list accepts on its own, so a stray `;` where a member
// should begin is still refused.
func (p *qmlParser) takeStatementEnd() {
	p.skipInlineSpace()
	p.sc.Take(";")
}

// skipInlineSpace consumes spaces and tabs but STOPS AT A NEWLINE, for the
// constructs that end at the end of their line.
func (p *qmlParser) skipInlineSpace() {
	for {
		r, ok := p.sc.Peek()
		if !ok || (r != ' ' && r != '\t' && r != '\r') {
			return
		}
		p.sc.Next()
	}
}

func (p *qmlParser) wanted(at parse.Position, want string) error {
	r, ok := p.sc.Peek()
	if !ok {
		return parse.SyntaxError{
			Format: "qml", Pos: at, Want: want, Got: "end of input", Incomplete: true,
		}
	}
	return parse.SyntaxError{Format: "qml", Pos: p.sc.Pos(), Want: want, Got: parse.QuoteRune(r)}
}

func (p *qmlParser) node() (*SpecNode, error) {
	startPos := p.sc.Pos()
	name, ok := p.ident()
	if !ok {
		r, ok := p.sc.Peek()
		if !ok {
			return nil, parse.SyntaxError{
				Format: "qml", Pos: startPos,
				Want: "a type name", Got: "end of input", Incomplete: true,
			}
		}
		return nil, parse.SyntaxError{
			Format: "qml", Pos: startPos,
			Want: "a type name", Got: parse.QuoteRune(r),
		}
	}
	// A qualified type — `T.Window` after `import tui 1.0 as T` — at the root
	// as anywhere else.
	for p.sc.HasPrefix(".") {
		dotAt := p.sc.Pos()
		p.sc.Take(".")
		seg, ok := p.ident()
		if !ok || !isTypeName(seg) {
			return nil, p.wanted(dotAt, "a type name after . in a qualified type")
		}
		name += "." + seg
	}
	return p.nodeBody(name, startPos)
}

// nodeBody parses `{ ... }` for a type name the caller has already read.
//
// It exists so the member loop can commit to a child node using the identifier
// it already consumed. The alternative — rewinding the scanner — would need
// arbitrary lookahead, and parse.Scanner.Unread is deliberately one token deep.
func (p *qmlParser) nodeBody(name string, startPos parse.Position) (*SpecNode, error) {
	if err := p.enter(startPos); err != nil {
		return nil, err
	}
	defer p.leave()

	if !isTypeName(name) {
		return nil, parse.SyntaxError{
			Format: "qml", Pos: startPos,
			Want: "a type name starting with an upper-case letter",
			Got:  parse.Quoted(name),
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
			return nil, parse.SyntaxError{
				Format: "qml", Pos: braceAt,
				Want: "{ to open " + name, Got: "end of input", Incomplete: true,
			}
		}
		return nil, parse.SyntaxError{
			Format: "qml", Pos: braceAt,
			Want: "{ to open " + name, Got: parse.QuoteRune(r),
		}
	}

	for {
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		if p.sc.Done() {
			return nil, parse.SyntaxError{
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
			return nil, parse.SyntaxError{
				Format: "qml", Pos: memberAt,
				Want: "a property, a handler, a child node, or }",
				Got:  parse.QuoteRune(r),
			}
		}

		// A property name may be DOTTED: `font.bold` groups a sub-object,
		// `Layout.fillWidth` attaches one. QML tells the two apart by whether
		// the first segment is capitalised — but only a consumer knows which
		// names it has registered as which, so this parser records the path and
		// rules on neither.
		path := []string{name}
		for p.sc.HasPrefix(".") {
			dotAt := p.sc.Pos()
			p.sc.Take(".")
			seg, ok := p.ident()
			if !ok {
				return nil, p.wanted(dotAt, "a name after . in the property name")
			}
			path = append(path, seg)
			name += "." + seg
		}

		// A child node is an identifier followed by `{`; a property or handler
		// is an identifier followed by `:`. One rune of lookahead separates
		// them, which is the whole reason the grammar spells a child as a
		// TypeName rather than something that needs backtracking.
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		switch {
		case p.sc.HasPrefix("{") && isTypeName(path[len(path)-1]):
			// QML capitalises TYPES, so `Text {` is a child node and `font {`
			// is a grouped property. That convention is the only thing
			// separating them, and it is the language's, not ours. It is the
			// LAST segment that decides: `T.Rectangle {` is a type reached
			// through a qualified import, `anchors {` a group.
			child, err := p.nodeBody(name, memberAt)
			if err != nil {
				return nil, err
			}
			n.Children = append(n.Children, child)

		case p.sc.HasPrefix("{"):
			// `font { bold: true }` — a grouped BLOCK. Parsed as its own shape
			// rather than flattened, because flattening would lose the
			// distinction between a block and the same names written as
			// separate dotted lines, and a consumer may legitimately care.
			if err := p.groupedBlock(n, path, memberAt); err != nil {
				return nil, err
			}

		case p.sc.Take(":"):
			if err := p.member(n, name, path, memberAt); err != nil {
				return nil, err
			}

		default:
			r, ok := p.sc.Peek()
			if !ok {
				return nil, parse.SyntaxError{
					Format: "qml", Pos: memberAt,
					Want: ": or { after " + parse.Quoted(name), Got: "end of input",
					Incomplete: true,
				}
			}
			return nil, parse.SyntaxError{
				Format: "qml", Pos: memberAt,
				Want: ": or { after " + parse.Quoted(name), Got: parse.QuoteRune(r),
			}
		}
	}
}

// member parses the right-hand side of `name:` — a handler when the name is an
// on-prefixed signal, a property otherwise.
func (p *qmlParser) member(n *SpecNode, name string, path []string, at parse.Position) error {
	if err := p.skipSpace(); err != nil {
		return err
	}

	if sig, ok := signalName(path[len(path)-1]); ok {
		body, err := p.handlerBody()
		if err != nil {
			return err
		}
		n.Handlers = append(n.Handlers, SpecHandler{
			Signal: sig, Body: body, Path: path, Pos: at})
		return nil
	}

	v, err := p.value()
	if err != nil {
		return err
	}
	p.takeStatementEnd()

	// `id` addresses the node rather than configuring it, so it is lifted out
	// of Props. It must be a bare identifier: an id that came from a call or a
	// binding could change between reloads, and an identity that moves is not
	// an identity.
	if path[0] == "id" && len(path) > 1 {
		return parse.SyntaxError{
			Format: "qml", Pos: at,
			Want: "id written as a plain name", Got: parse.Quoted(name),
		}
	}
	if name == "id" {
		if v.Kind != SpecValueRef || len(v.Path) > 1 {
			return parse.SyntaxError{
				Format: "qml", Pos: v.Pos,
				Want: "a bare identifier for id",
				Got:  v.Kind.String(),
			}
		}
		if n.ID != "" {
			return parse.SyntaxError{
				Format: "qml", Pos: at,
				Want: "at most one id per node",
				Got:  "a second id",
			}
		}
		n.ID = v.Raw
		return nil
	}

	n.Props = append(n.Props, SpecProp{Name: name, Path: path, Value: v, Pos: at})
	return nil
}

// handlerBody parses the right-hand side of an `onSignal:` — JavaScript, which
// is what QML puts there.
//
// Both QML forms are the same grammar: `onClicked: save()` is one expression
// statement and `onClicked: { let x = 1; save(x) }` is a block, so the block
// form needs no special case. What this DOES NOT do is decide whether a body is
// something an evaluator can run — a bare `save` parses as the identifier
// expression it is, and an engine that only invokes named handlers refuses it by
// name and position. The parser's job is the language, not its consumer's reach.
//
// It runs on the QML parser's OWN scanner and depth budget, so a handler body
// cannot smuggle in recursion the document's limit was meant to bound.
func (p *qmlParser) handlerBody() ([]js.Stmt, error) {
	if err := p.skipSpace(); err != nil {
		return nil, err
	}
	at := p.sc.Pos()
	if _, ok := p.sc.Peek(); !ok {
		return nil, parse.SyntaxError{
			Format: "qml", Pos: at,
			Want: "a handler body", Got: "end of input", Incomplete: true,
		}
	}

	// The body starts at the depth the DOCUMENT has already spent, and is capped
	// by the document's own limit. That seeding is the whole mechanism: a body
	// three nodes down has three levels less to spend than one at the root, so
	// nesting is bounded across the two grammars rather than within each.
	//
	// There is deliberately nothing to carry back. enter/leave are balanced, so
	// a body that returns has left the counter where it found it — and an
	// assignment here would be a line that looks load-bearing and is not.
	dr := js.NewDriver(p.sc, &js.JavaScript, p.maxDepth, p.depth)
	st, err := dr.Statement()
	if err != nil {
		return nil, err
	}
	if st.Kind == js.StmtBlock {
		return st.Body, nil
	}
	return []js.Stmt{st}, nil
}

// value parses one property value.
func (p *qmlParser) value() (SpecValue, error) {
	if err := p.skipSpace(); err != nil {
		return SpecValue{}, err
	}
	at := p.sc.Pos()

	r, ok := p.sc.Peek()
	if !ok {
		return SpecValue{}, parse.SyntaxError{
			Format: "qml", Pos: at,
			Want: "a value", Got: "end of input", Incomplete: true,
		}
	}

	_ = r

	// A property value is a JavaScript expression, because in QML that is what
	// a property value IS. ONE parser reads it — an earlier design had a small
	// hand-written value grammar beside this, which is two parsers of the same
	// thing and therefore two answers waiting to disagree.
	//
	// The budget is seeded from the document's, exactly as a handler body's is:
	// what has to stay bounded is the recursion, and it does not care which
	// grammar recursed.
	dr := js.NewDriver(p.sc, &js.JavaScript, p.maxDepth, p.depth)
	e, err := dr.Expression()
	if err != nil {
		return SpecValue{}, err
	}
	return projectValue(&e), nil
}

// QML property values are parsed with the PLAIN JavaScript dialect.
//
// This grammar has no extensions of its own any more. It briefly had one —
// `@name`, a symbolic style value — and it is gone: a style token is a name on
// an object the host provides, which QML already spells `Theme.surface`, so the
// sigil was a second way to write something the language already had.
//
// projectValue reduces an expression to the terminal shapes this format names,
// and keeps the tree for everything else.
//
// The projection is not a simplification of the grammar; it is a CONVENIENCE
// for consumers, which ask "is this a string" far more often than they walk a
// tree. Every shape it does not name survives intact as [SpecValueExpr], so
// nothing is lost by projecting and nothing downstream has to reparse.
func projectValue(e *js.Expr) SpecValue {
	switch e.Kind {
	case js.ExprString:
		return SpecValue{Kind: SpecValueString, Raw: e.Raw, Pos: e.Pos}
	case js.ExprNumber:
		return SpecValue{Kind: SpecValueNumber, Raw: e.Raw, Pos: e.Pos}
	case js.ExprBool:
		return SpecValue{Kind: SpecValueBool, Raw: e.Raw, Pos: e.Pos}

	case js.ExprUnary:
		// JavaScript has no negative literals: `-3` is unary minus applied to
		// 3. Folding the sign back onto the literal is what lets a consumer
		// keep reading `neg: -3` as the number it obviously is.
		if (e.Raw == "-" || e.Raw == "+") && e.Left != nil && e.Left.Kind == js.ExprNumber {
			raw := e.Left.Raw
			if e.Raw == "-" {
				raw = "-" + raw
			}
			return SpecValue{Kind: SpecValueNumber, Raw: raw, Pos: e.Pos}
		}

	case js.ExprIdent, js.ExprMember:
		if path, ok := dottedPath(e); ok {
			return SpecValue{Kind: SpecValueRef, Raw: joinPath(path), Path: path, Pos: e.Pos}
		}

	case js.ExprCall:
		path, ok := dottedPath(e.Left)
		if !ok {
			break
		}
		args := make([]SpecValue, 0, len(e.Args))
		for i := range e.Args {
			av := projectValue(&e.Args[i])
			if av.Kind == SpecValueExpr {
				// An argument that did not project would leave a Call whose
				// Args are a different shape from the call itself. The whole
				// node keeps its tree instead, so a consumer reading Args never
				// meets a half-projected one.
				return SpecValue{Kind: SpecValueExpr, Raw: e.Raw, Expr: e, Pos: e.Pos}
			}
			args = append(args, av)
		}
		return SpecValue{Kind: SpecValueCall, Raw: joinPath(path), Path: path,
			Args: args, Pos: e.Pos}
	}

	return SpecValue{Kind: SpecValueExpr, Raw: e.Raw, Expr: e, Pos: e.Pos}
}

// ProjectValue is the projection a property value gets, for a consumer
// holding a subexpression of one: the operands of `Dialog.Yes | Dialog.No` are
// themselves values, projected exactly as a whole property's would be.
func ProjectValue(e *js.Expr) SpecValue { return projectValue(e) }

// dottedPath flattens an identifier or a non-computed member chain to its
// segments, reporting false for anything else.
//
// A COMPUTED member — `items[i]` — is deliberately not a path: which member it
// reaches is decided when it runs, so it is not a name anything can resolve
// ahead of time.
func dottedPath(e *js.Expr) ([]string, bool) {
	if e == nil {
		return nil, false
	}
	switch e.Kind {
	case js.ExprIdent:
		return []string{e.Raw}, true
	case js.ExprMember:
		if e.Computed {
			return nil, false
		}
		left, ok := dottedPath(e.Left)
		if !ok {
			return nil, false
		}
		return append(left, e.Name), true
	default:
		return nil, false
	}
}

func joinPath(p []string) string {
	s := ""
	for i, seg := range p {
		if i > 0 {
			s += "."
		}
		s += seg
	}
	return s
}

// The hand-written value grammar that used to live here — refValue, callValue,
// stringValue and numberValue — is GONE. A property value is a JavaScript
// expression and one parser reads it; these were the second parser of the same
// thing, kept alive only by the branch in value() that no longer exists.
//
// Nothing referenced them, which is exactly why they were easy to miss: Go does
// not report an unused method, and a test suite cannot fail because of code it
// never reaches. The coverage gate found them.

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
					return parse.SyntaxError{
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
