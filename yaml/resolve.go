package yaml

import (
	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
)

// Resolve assigns each node of doc its tag under schema, the spec's tag resolution step. A node
// with an explicit tag keeps it; a non-specific one is resolved by the schema's rules. Under
// Failsafe, nodes with the "?" non-specific tag stay unresolved and are absent from the result.
// Under JSON, a plain scalar matching none of its formats is an error.
func Resolve(doc *pyaml.Document, schema Schema) (Tags, error) {
	tags := Tags{}
	if doc == nil || doc.Root == nil {
		return tags, nil
	}
	r := resolver{doc: doc, schema: schema, tags: tags}
	if err := r.node(doc.Root); err != nil {
		return nil, err
	}
	// an alias carries its target's tag
	for n := range r.aliases {
		if t, ok := tags[n.Target]; ok {
			tags[n] = t
		}
	}
	return tags, nil
}

type resolver struct {
	doc     *pyaml.Document
	schema  Schema
	tags    Tags
	aliases map[*pyaml.Node]bool
}

// nonSpecific is the non-specific tag an untagged node has (spec 6.9.1): "?" for a plain scalar or
// a collection, "!" for any other scalar; or the "!" written explicitly.
func nonSpecific(n *pyaml.Node) string {
	if n.Tag == "!" {
		return "!"
	}
	if n.Kind == pyaml.KindScalar && n.Style != pyaml.StylePlain {
		return "!"
	}
	return "?"
}

func byKind(n *pyaml.Node) string {
	switch n.Kind {
	case pyaml.KindSequence:
		return TagSeq
	case pyaml.KindMapping:
		return TagMap
	}
	return TagStr
}

func (r *resolver) node(n *pyaml.Node) error {
	switch n.Kind {
	case pyaml.KindAlias:
		if r.aliases == nil {
			r.aliases = map[*pyaml.Node]bool{}
		}
		r.aliases[n] = true
		return nil
	case pyaml.KindSequence:
		for _, c := range n.Items {
			if err := r.node(c); err != nil {
				return err
			}
		}
	case pyaml.KindMapping:
		for _, p := range n.Pairs {
			if err := r.node(p.Key); err != nil {
				return err
			}
			if err := r.node(p.Value); err != nil {
				return err
			}
		}
	}
	if n.Tag != "" && n.Tag != "!" {
		r.tags[n] = n.Tag
		return nil
	}
	if nonSpecific(n) == "!" {
		r.tags[n] = byKind(n)
		return nil
	}
	switch {
	case r.schema == Failsafe:
		return nil // "?" stays unresolved
	case n.Kind != pyaml.KindScalar:
		r.tags[n] = byKind(n)
		return nil
	}
	tag, ok := plainTag(n.Value, r.schema)
	if !ok {
		return &Error{Pos: r.doc.Position(n.Span.Start), Msg: "the plain scalar \"" + clip(n.Value) + "\" matches no JSON schema format"}
	}
	r.tags[n] = tag
	return nil
}

// plainTag resolves a plain scalar by the schema's table: the first format it matches wins.
func plainTag(v []byte, schema Schema) (string, bool) {
	s := string(v)
	if schema == JSON {
		switch {
		case s == "null":
			return TagNull, true
		case s == "true" || s == "false":
			return TagBool, true
		case jsonInt(s):
			return TagInt, true
		case jsonFloat(s):
			return TagFloat, true
		}
		return "", false
	}
	switch {
	case coreNull(s):
		return TagNull, true
	case coreBool(s):
		return TagBool, true
	case coreInt(s):
		return TagInt, true
	case coreFloat(s):
		return TagFloat, true
	}
	return TagStr, true
}

func clip(v []byte) string {
	if len(v) > 40 {
		return string(v[:40]) + "…"
	}
	return string(v)
}

func isDigits(s string, ok func(byte) bool) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

func dec(c byte) bool { return c >= '0' && c <= '9' }
func oct(c byte) bool { return c >= '0' && c <= '7' }
func hex(c byte) bool { return dec(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }

// jsonInt is -?(0|[1-9][0-9]*).
func jsonInt(s string) bool {
	s = trimPrefix(s, "-")
	return s == "0" || (len(s) > 0 && s[0] >= '1' && s[0] <= '9' && isDigits(s, dec))
}

// jsonFloat is -?(0|[1-9][0-9]*)(\.[0-9]*)?([eE][-+]?[0-9]+)?.
func jsonFloat(s string) bool {
	s = trimPrefix(s, "-")
	i := 0
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && dec(s[i]) {
			i++
		}
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && dec(s[i]) {
			i++
		}
	}
	return exponent(s[i:])
}

// exponent is ([eE][-+]?[0-9]+)?, the whole of s.
func exponent(s string) bool {
	if s == "" {
		return true
	}
	if s[0] != 'e' && s[0] != 'E' {
		return false
	}
	s = s[1:]
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	return isDigits(s, dec)
}

func trimPrefix(s, p string) string {
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):]
	}
	return s
}

func coreNull(s string) bool { return s == "" || s == "~" || s == "null" || s == "Null" || s == "NULL" }

func coreBool(s string) bool {
	switch s {
	case "true", "True", "TRUE", "false", "False", "FALSE":
		return true
	}
	return false
}

// coreInt is [-+]?[0-9]+, 0o[0-7]+ or 0x[0-9a-fA-F]+.
func coreInt(s string) bool {
	switch {
	case len(s) > 2 && s[:2] == "0o":
		return isDigits(s[2:], oct)
	case len(s) > 2 && s[:2] == "0x":
		return isDigits(s[2:], hex)
	}
	if s != "" && (s[0] == '-' || s[0] == '+') {
		s = s[1:]
	}
	return isDigits(s, dec)
}

// coreFloat is [-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?, [-+]?\.(inf|Inf|INF), or
// \.(nan|NaN|NAN).
func coreFloat(s string) bool {
	switch s {
	case ".nan", ".NaN", ".NAN":
		return true
	}
	if s != "" && (s[0] == '-' || s[0] == '+') {
		s = s[1:]
	}
	switch s {
	case ".inf", ".Inf", ".INF":
		return true
	}
	i := 0
	for i < len(s) && dec(s[i]) {
		i++
	}
	digits := i > 0
	if i < len(s) && s[i] == '.' {
		i++
		j := i
		for i < len(s) && dec(s[i]) {
			i++
		}
		if !digits && i == j {
			return false // "." alone, or "." before the exponent
		}
		digits = true
	}
	return digits && exponent(s[i:])
}
