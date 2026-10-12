package qml

import (
	"github.com/yongjohnlee80/golib/parse"
)

// DECLARATIONS — a node's own properties.
//
//	Dialog {
//	    property string label: "Find"
//	    required property var model
//	    default property alias columns: list.data
//	    property alias footer: footerRow.data
//	    TableView { id: list; model: model }
//	    Row { id: footerRow }
//	}
//
// The parser records what was declared and how it was spelled; what a
// declaration MEANS — that a use of the component sets it, that a required one
// must be set — is the consumer's (decl's components). Three Qt spellings are
// refused here rather than recorded, because no consumer of this tree does
// anything with them, and a refusal that names the alternative is cheaper than
// one three files later.

// declTypes are the property types a declaration may name, as Qt spells them.
// `list<T>` is checked apart, since T is any type name.
var declTypes = map[string]bool{
	"string": true, "int": true, "real": true, "double": true,
	"bool": true, "var": true, "url": true, "color": true,
}

const declTypeList = "string, int, real, double, bool, var, url, color or list<T>"

// isDeclWord reports whether a member's first word may open a declaration.
func isDeclWord(w string) bool {
	switch w {
	case "property", "required", "default", "readonly", "signal":
		return true
	}
	return false
}

// declaration parses a declaration whose first word the member loop has read,
// and appends it to n. at is where that word began.
func (p *qmlParser) declaration(n *SpecNode, word string, at parse.Position) error {
	switch word {
	case "signal":
		return parse.SyntaxError{
			Format: "qml", Pos: at,
			Want: "a call to an App handler with a declared property's value, " +
				"since a component declares no signal",
			Got: parse.Quoted("signal"),
		}
	case "readonly":
		return parse.SyntaxError{
			Format: "qml", Pos: at,
			Want: "property <type> <name> without readonly, since a component's " +
				"property is set by its use site and nothing else writes it",
			Got: parse.Quoted("readonly"),
		}
	}

	d := SpecDecl{Pos: at, Required: word == "required"}
	if word != "property" {
		kwAt := p.sc.Pos()
		if kw, ok := p.ident(); !ok || kw != "property" {
			if !ok {
				return p.wanted(kwAt, "property after "+word)
			}
			return parse.SyntaxError{Format: "qml", Pos: kwAt,
				Want: "property after " + word, Got: parse.Quoted(kw)}
		}
		if err := p.skipSpace(); err != nil {
			return err
		}
	}

	typ, err := p.declType()
	if err != nil {
		return err
	}
	if typ == "alias" {
		return p.alias(n, d, word == "default", at)
	}
	if word == "default" {
		return parse.SyntaxError{Format: "qml", Pos: at,
			Want: "default property alias <name>: <id>.data, the one default property a component declares",
			Got:  parse.Quoted("default property " + typ)}
	}
	d.Type = typ

	if err := p.skipSpace(); err != nil {
		return err
	}
	if d.Name, err = p.declName(); err != nil {
		return err
	}

	if err := p.skipSpace(); err != nil {
		return err
	}
	if p.sc.HasPrefix(":") {
		if d.Required {
			return parse.SyntaxError{Format: "qml", Pos: p.sc.Pos(),
				Want: "no value for a required property: its use site gives one, or drop required " +
					"to give a default",
				Got: parse.QuoteRune(':')}
		}
		p.sc.Take(":")
		v, err := p.value()
		if err != nil {
			return err
		}
		d.Default = &v
	}
	p.takeStatementEnd()
	n.Decls = append(n.Decls, d)
	return nil
}

// declType reads a declaration's type: one of [declTypes], `list<T>`, or
// `alias` for the caller to judge.
func (p *qmlParser) declType() (string, error) {
	at := p.sc.Pos()
	typ, ok := p.ident()
	if !ok {
		return "", p.wanted(at, "a property type: "+declTypeList)
	}
	switch {
	case typ == "alias" || declTypes[typ]:
		return typ, nil
	case typ == "list":
		if !p.sc.Take("<") {
			return "", p.wanted(p.sc.Pos(), "< after list, as in list<string>")
		}
		elemAt := p.sc.Pos()
		elem, ok := p.ident()
		if !ok {
			return "", p.wanted(elemAt, "a type name in list<…>")
		}
		for p.sc.HasPrefix(".") {
			dotAt := p.sc.Pos()
			p.sc.Take(".")
			seg, ok := p.ident()
			if !ok {
				return "", p.wanted(dotAt, "a name after . in list<…>")
			}
			elem += "." + seg
		}
		if !p.sc.Take(">") {
			return "", p.wanted(p.sc.Pos(), "> to close list<"+elem)
		}
		return "list<" + elem + ">", nil
	}
	return "", parse.SyntaxError{Format: "qml", Pos: at,
		Want: "a property type: " + declTypeList, Got: parse.Quoted(typ)}
}

// declName reads a declared property's name. QML's rules hold: it starts
// lower-case, and it is neither `id` nor a handler's spelling.
func (p *qmlParser) declName() (string, error) {
	at := p.sc.Pos()
	name, ok := p.ident()
	if !ok {
		return "", p.wanted(at, "a property name after the type")
	}
	if _, handler := signalName(name); isTypeName(name) || handler || name == "id" {
		return "", parse.SyntaxError{Format: "qml", Pos: at,
			Want: "a property name starting with a lower-case letter, and not id or on<Signal>",
			Got:  parse.Quoted(name)}
	}
	return name, nil
}

// alias reads the rest of `[default] property alias <name>: <id>.data`. The
// default one takes the use site's children; a named one, a slot, takes the
// object its use site sets it to. An alias to anything but an object's data is
// refused, naming the plain property that says the same (refuseAlias).
func (p *qmlParser) alias(n *SpecNode, d SpecDecl, isDefault bool, at parse.Position) error {
	d.Type = "alias"
	if err := p.skipSpace(); err != nil {
		return err
	}
	var err error
	if d.Name, err = p.declName(); err != nil {
		return err
	}
	if err := p.skipSpace(); err != nil {
		return err
	}
	kind := "property alias "
	if isDefault {
		kind = "default " + kind
	}
	want := "<id>.data after " + kind
	if !p.sc.Take(":") {
		return p.wanted(p.sc.Pos(), ": "+want+d.Name)
	}
	if err := p.skipSpace(); err != nil {
		return err
	}
	targetAt := p.sc.Pos()
	target, ok := p.ident()
	if !ok {
		return p.wanted(targetAt, want+d.Name+":")
	}
	if !p.sc.Take(".") {
		return p.wanted(p.sc.Pos(), want+d.Name+":")
	}
	leafAt := p.sc.Pos()
	leaf, ok := p.ident()
	if !ok {
		return p.wanted(leafAt, want+d.Name+":")
	}
	if leaf != "data" || p.sc.HasPrefix(".") {
		path := target + "." + leaf
		for p.sc.Take(".") {
			seg, ok := p.ident()
			if !ok {
				break
			}
			path += "." + seg
		}
		if !isDefault {
			return refuseAlias(at, d.Name, path)
		}
		return parse.SyntaxError{Format: "qml", Pos: targetAt,
			Want: want + d.Name + ": the use site's children go to an object's data",
			Got:  parse.Quoted(path)}
	}
	if d.Required {
		return parse.SyntaxError{Format: "qml", Pos: at,
			Want: "property alias " + d.Name + ": " + target + ".data without required: a slot " +
				"its use site leaves unset is empty",
			Got: parse.Quoted("required property alias")}
	}
	d.Target, d.Slot = target, !isDefault
	p.takeStatementEnd()
	n.Decls = append(n.Decls, d)
	return nil
}

// refuseAlias refuses `property alias x: a.b` where b is not data, naming the
// plain property that says the same thing.
func refuseAlias(at parse.Position, name, path string) error {
	return parse.SyntaxError{Format: "qml", Pos: at,
		Want: "property <type> " + name + ", used where " + path + " is set, since a " +
			"component's alias is to an object's data (a slot) and to nothing else",
		Got: parse.Quoted("property alias")}
}
