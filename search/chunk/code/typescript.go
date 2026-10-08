package code

import (
	"strings"

	"github.com/yongjohnlee80/golib/search"
)

// TypeScript chunks TypeScript and JavaScript (.ts, .tsx, .js, .jsx, .mjs) by declaration:
// functions, classes and their methods, interfaces, type aliases, and exported const arrow
// functions, each with its JSDoc and decorators. Imports and other top-level statements fall into
// the header and glue chunks.
type TypeScript struct{}

const tsVersion = "code-ts-2"

func (TypeScript) Version() string { return tsVersion }

func (TypeScript) Chunk(d search.Doc) ([]search.Chunk, error) {
	t := newSrc(d.Text, tsSyntax())
	units := tsUnits(t, d.Path)
	return assemble(d.Text, units, d.Path, d.Path+" > (glue)", d.Tokens), nil
}

func isJSDoc(c string) bool { return strings.HasPrefix(c, "/**") }

// tsDecorator skips one decorator at i (@name.path(args)), returning where it ends, or i.
func tsDecorator(t src) func(int) int {
	return func(i int) int {
		if !t.is(i, '@') {
			return i
		}
		k := i + 1
		for {
			_, e := t.word(k)
			if e == k {
				return i
			}
			k = e
			if t.is(k, '.') {
				k++
				continue
			}
			break
		}
		if t.is(k, '(') {
			k = t.match(k)
		}
		return k
	}
}

func tsUnits(t src, file string) []unit {
	var units []unit
	i := 0
	for i < len(t.b) {
		start, doc, k := t.prefix(i, tsDecorator(t), isJSDoc)
		if k >= len(t.b) {
			break
		}
		if !t.code(k) {
			i = k + 1
			continue
		}
		mods := k
		w, e := t.word(k)
		for w == "export" || w == "default" || w == "declare" || w == "abstract" || w == "async" {
			k = t.skip(e)
			w, e = t.word(k)
		}
		exported := strings.HasPrefix(string(t.b[mods:k]), "export")
		var us []unit
		end := -1
		switch w {
		case "function":
			us, end = tsFunction(t, file, start, mods, e, doc)
		case "class":
			us, end = tsClass(t, file, start, mods, e, doc)
		case "interface":
			name, ne := t.word(t.skip(e))
			if b := t.findCode(ne, len(t.b), "{"); b >= 0 {
				end = t.match(b)
				crumb := file + " > " + name
				us = []unit{{start: start, end: end, crumb: crumb,
					embed: joinNonEmpty(crumb, doc, string(t.b[mods:end]))}}
			}
		case "type":
			name, ne := t.word(t.skip(e))
			if name != "" && (t.is(t.skip(ne), '=') || t.is(t.skip(ne), '<')) {
				end = t.statementEnd(ne)
				crumb := file + " > " + name
				us = []unit{{start: start, end: end, crumb: crumb,
					embed: joinNonEmpty(crumb, doc, string(t.b[mods:end]))}}
			}
		case "const", "let", "var":
			if exported {
				us, end = tsArrow(t, file, start, mods, e, doc)
			}
		}
		if end > k && len(us) > 0 {
			units = append(units, us...)
			i = end
			continue
		}
		// not a unit: skip the statement whole; it falls into the header or a glue chunk
		next := t.statementEnd(k)
		if next <= k {
			next = k + 1
		}
		i = next
	}
	return units
}

// tsBody finds a function's body brace after its parameters at p (the "("): a "{" right after a
// ":" or "=>" in the return type is a type literal, not the body.
func tsBody(t src, p int) int {
	k := t.match(p)
	for k < len(t.b) {
		k = t.skip(k)
		if k >= len(t.b) || !t.code(k) {
			return -1
		}
		switch t.b[k] {
		case '{':
			if prev := t.lastCodeBefore(k); prev == ':' || prev == '|' || prev == '&' || prev == ',' || prev == '<' {
				k = t.match(k)
				continue
			}
			return k
		case ';', '}':
			return -1
		case '(', '[', '<':
			if t.b[k] == '<' {
				k++
				continue
			}
			k = t.match(k)
		default:
			k++
		}
	}
	return -1
}

func tsFunction(t src, file string, start, mods, e int, doc string) ([]unit, int) {
	k := t.skip(e)
	if t.is(k, '*') {
		k = t.skip(k + 1)
	}
	name, ne := t.word(k)
	p := t.findCode(ne, len(t.b), "(")
	if p < 0 {
		return nil, -1
	}
	body := tsBody(t, p)
	if body < 0 {
		end := t.statementEnd(p)
		crumb := file + " > " + name
		return []unit{{start: start, end: end, crumb: crumb, embed: joinNonEmpty(crumb, doc, string(t.b[mods:end]))}}, end
	}
	end := t.match(body)
	return []unit{braceUnit(t, file+" > "+name, start, mods, body, end, doc, "const", "let", "var", "function")}, end
}

// braceUnit is a declaration whose body is the braces at [body, end): its embedding is the doc and
// the signature, never the body, and its statements are the places an oversized one is cut.
func braceUnit(t src, crumb string, start, sigStart, body, end int, doc string, decl ...string) unit {
	sig := strings.TrimSpace(string(t.b[sigStart:body]))
	return unit{
		start: start, end: end, crumb: crumb, sig: oneLine(sig), tail: sig,
		embed:     joinNonEmpty(crumb, doc, sig),
		cuts:      t.lineCuts(body+1, end-1),
		fragEmbed: func(s, e int) string { return joinNonEmpty(oneLine(sig), t.summary(s, e, decl...)) },
	}
}

func tsArrow(t src, file string, start, mods, e int, doc string) ([]unit, int) {
	name, ne := t.word(t.skip(e))
	eq := t.findCode(ne, len(t.b), "=;\n")
	if eq < 0 || t.b[eq] != '=' {
		return nil, -1
	}
	v := t.skip(eq + 1)
	if w, we := t.word(v); w == "async" {
		v = t.skip(we)
	}
	arrow := -1
	switch {
	case t.is(v, '('):
		a := t.skip(t.match(v))
		if t.is(a, ':') { // a return type before the arrow
			a = t.findCode(a, len(t.b), "=")
		}
		if a >= 0 && t.is(a, '=') && t.is(a+1, '>') {
			arrow = a + 2
		}
	default:
		if w, we := t.word(v); w == "function" {
			_ = we
			return tsFunction(t, file, start, mods, v+len("function"), doc)
		} else if w != "" {
			if a := t.skip(we); t.is(a, '=') && t.is(a+1, '>') {
				arrow = a + 2
			}
		}
	}
	if arrow < 0 {
		return nil, -1
	}
	b := t.skip(arrow)
	crumb := file + " > " + name
	if t.is(b, '{') {
		end := t.match(b)
		if t.is(t.skipInline(end), ';') {
			end = t.skipInline(end) + 1
		}
		return []unit{braceUnit(t, crumb, start, mods, b, end, doc, "const", "let", "var", "function")}, end
	}
	end := t.statementEnd(b)
	return []unit{{start: start, end: end, crumb: crumb, sig: oneLine(string(t.b[mods:arrow])),
		embed: joinNonEmpty(crumb, doc, string(t.b[mods:end]))}}, end
}

// tsClass is a class: a head unit (the declaration and anything before its first method), a unit
// per method, and the fields between methods as units of the class. The closing brace stays with
// the last of them, so the class's span is partitioned.
func tsClass(t src, file string, start, mods, e int, doc string) ([]unit, int) {
	name, _ := t.word(t.skip(e))
	b := t.findCode(e, len(t.b), "{")
	if b < 0 {
		return nil, -1
	}
	end := t.match(b)
	crumb := file + " > " + name
	var members []unit
	k := b + 1
	for k < end-1 {
		ms, mdoc, mk := t.prefix(k, tsDecorator(t), isJSDoc)
		if mk >= end-1 {
			break
		}
		// modifiers, then a name, then "(" for a method
		n := mk
		w, we := t.word(n)
		for isTSModifier(w) && t.code(t.skip(we)) && !t.is(t.skip(we), '(') && !t.is(t.skip(we), '=') && !t.is(t.skip(we), ':') {
			n = t.skip(we)
			w, we = t.word(n)
		}
		if t.is(n, '*') || t.is(n, '#') {
			n++
			w, we = t.word(n)
		}
		after := t.skip(we)
		if t.is(after, '<') {
			after = t.findCode(after, end, "(")
		}
		if w != "" && after >= 0 && t.is(after, '(') {
			if body := tsBody(t, after); body >= 0 && body < end {
				mend := t.match(body)
				members = append(members, braceUnit(t, crumb+" > "+w, ms, mk, body, mend, mdoc, "const", "let", "var"))
				k = mend
				continue
			}
		}
		next := t.statementEnd(mk)
		if next <= mk || next > end-1 {
			break
		}
		k = next
	}
	headEnd := end
	if len(members) > 0 {
		headEnd = members[0].start
	}
	head := unit{start: start, end: headEnd, crumb: crumb, tail: oneLine(string(t.b[mods:b])),
		embed: joinNonEmpty(crumb, doc, oneLine(string(t.b[mods:b])))}
	units := []unit{head}
	for i, m := range members {
		if i > 0 && hasCode(t, units[len(units)-1].end, m.start) {
			// fields between two methods: a unit of the class
			s, e := trimSpan(t.b, units[len(units)-1].end, m.start)
			units = append(units, unit{start: s, end: e, crumb: crumb})
		}
		units = append(units, m)
	}
	units[len(units)-1].end = end
	return units, end
}

func hasCode(t src, s, e int) bool {
	a, b := trimSpan(t.b, s, e)
	return a < b
}

func isTSModifier(w string) bool {
	switch w {
	case "public", "private", "protected", "static", "async", "readonly", "get", "set", "abstract", "override", "declare":
		return true
	}
	return false
}
