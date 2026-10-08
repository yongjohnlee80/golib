package code

import (
	"strings"

	"github.com/yongjohnlee80/golib/search"
)

// Python chunks Python (.py) by declaration: def, async def and class, found by indentation, with
// their decorators; a class's methods are units of their own. A function's docstring is its doc.
// Top-level statements fall into the header and glue chunks.
type Python struct{}

const pyVersion = "code-py-2"

func (Python) Version() string { return pyVersion }

func (Python) Chunk(d search.Doc) ([]search.Chunk, error) {
	t := newSrc(d.Text, pySyntax())
	p := pyFile{src: t, lines: pyLines(t)}
	units := p.block(0, len(p.lines), 0, d.Path)
	return assemble(d.Text, units, d.Path, d.Path+" > (glue)", d.Tokens), nil
}

// pyLine is one source line: where it starts and ends (before its newline), its indentation, and
// whether its first non-blank byte is code (not inside a string that began above, not a comment).
type pyLine struct {
	start, end, first int
	indent            int
	blank, code       bool
	comment           bool
}

type pyFile struct {
	src   src // a field, not embedded: golib's bases are never promoted
	lines []pyLine
}

func pyLines(t src) []pyLine {
	var out []pyLine
	for s := 0; s < len(t.b); {
		e := s
		for e < len(t.b) && t.b[e] != '\n' {
			e++
		}
		l := pyLine{start: s, end: e, first: s}
		for l.first < e && (t.b[l.first] == ' ' || t.b[l.first] == '\t') {
			l.first++
		}
		l.indent = l.first - s
		l.blank = l.first >= e || t.b[l.first] == '\r'
		if !l.blank {
			l.code = t.code(l.first)
			l.comment = t.m[l.first] == inComment
		}
		out = append(out, l)
		s = e + 1
	}
	return out
}

// startsDecl reports a line that begins a def, async def, class or decorator.
func (p pyFile) startsDecl(l pyLine) bool {
	if !l.code {
		return false
	}
	if p.src.b[l.first] == '@' {
		return true
	}
	w, e := p.src.word(l.first)
	if w == "async" {
		w, _ = p.src.word(p.src.skipInline(e))
	}
	return w == "def" || w == "class"
}

// block finds the declarations among lines [from, to) at indentation indent: each runs from its
// first decorator to its last line deeper than indent (a comment line counts only when code at
// that depth follows it), and a class's body is searched for its methods.
func (p pyFile) block(from, to, indent int, crumb string) []unit {
	var units []unit
	for i := from; i < to; i++ {
		l := p.lines[i]
		if l.blank || !l.code || l.indent != indent || !p.startsDecl(l) {
			continue
		}
		start := l.first
		// past the decorators to the def or class line
		j := i
		for j < to && p.lines[j].code && p.lines[j].indent == indent && p.src.b[p.lines[j].first] == '@' {
			e := p.lines[j].end
			if par := p.src.findCode(p.lines[j].first, p.lines[j].end, "("); par >= 0 {
				e = max(e, p.src.match(par)-1) // a decorator's arguments may span lines
			}
			j = lineOf(p.lines, e) + 1
			for j < to && (p.lines[j].blank || p.lines[j].comment) {
				j++
			}
		}
		if j >= to {
			break
		}
		decl := p.lines[j]
		w, we := p.src.word(decl.first)
		if w == "async" {
			w, we = p.src.word(p.src.skipInline(we))
		}
		if w != "def" && w != "class" {
			continue
		}
		name, _ := p.src.word(p.src.skipInline(we))
		// the header ends at the ":" closing the def/class line (parameters may span lines)
		colon := p.src.findCode(we, len(p.src.b), ":")
		if colon < 0 {
			continue
		}
		last := j
		k := lineOf(p.lines, colon) + 1
		for ; k < to; k++ {
			lk := p.lines[k]
			if lk.blank {
				continue
			}
			if !lk.code && !lk.comment { // inside a multi-line string: the body's
				last = k
				continue
			}
			if lk.indent > indent {
				last = k
				continue
			}
			if lk.comment && p.deeperFollows(k+1, to, indent) {
				last = k
				continue
			}
			break
		}
		if last < lineOf(p.lines, colon) {
			last = lineOf(p.lines, colon)
		}
		end := p.lines[last].end
		ucrumb := crumb + " > " + name
		sig := strings.TrimSpace(string(p.src.b[decl.first:colon]))
		doc := p.docstring(colon+1, end)
		decorators := strings.TrimSpace(string(p.src.b[start:decl.first]))
		bodyIndent := p.bodyIndent(lineOf(p.lines, colon)+1, last+1, indent)
		if w == "class" {
			members := p.block(lineOf(p.lines, colon)+1, last+1, bodyIndent, ucrumb)
			units = append(units, pyContainer(p, ucrumb, start, end, decorators, sig, doc, members)...)
		} else {
			u := unit{start: start, end: end, crumb: ucrumb, sig: oneLine(sig), tail: sig,
				embed: joinNonEmpty(ucrumb, doc, decorators, sig)}
			for m := lineOf(p.lines, colon) + 1; m <= last; m++ {
				if lm := p.lines[m]; !lm.blank && (lm.code || lm.comment) && lm.indent == bodyIndent && m > lineOf(p.lines, colon)+1 {
					u.cuts = append(u.cuts, lm.first)
				}
			}
			sigCopy := sig
			u.fragEmbed = func(s, e int) string { return joinNonEmpty(oneLine(sigCopy), p.src.summary(s, e, "def", "class")) }
			units = append(units, u)
		}
		i = last
	}
	return units
}

// pyContainer is a class: its head (decorators, the class line and anything before the first
// method), each method, and the statements between methods as units of the class, the last taking
// the class's remaining lines, so its span is partitioned.
func pyContainer(p pyFile, crumb string, start, end int, decorators, sig, doc string, members []unit) []unit {
	headEnd := end
	if len(members) > 0 {
		headEnd = members[0].start
	}
	units := []unit{{start: start, end: headEnd, crumb: crumb, tail: sig, embed: joinNonEmpty(crumb, doc, decorators, sig)}}
	for i, m := range members {
		if i > 0 && hasCode(p.src, units[len(units)-1].end, m.start) {
			s, e := trimSpan(p.src.b, units[len(units)-1].end, m.start)
			units = append(units, unit{start: s, end: e, crumb: crumb})
		}
		units = append(units, m)
	}
	units[len(units)-1].end = end
	return units
}

// deeperFollows reports whether the next code line after k is indented deeper than indent: a
// comment line at the margin inside a function body belongs to it only then.
func (p pyFile) deeperFollows(k, to, indent int) bool {
	for ; k < to; k++ {
		l := p.lines[k]
		if l.blank || l.comment {
			continue
		}
		return l.indent > indent || !l.code
	}
	return false
}

func (p pyFile) bodyIndent(from, to, indent int) int {
	for k := from; k < to; k++ {
		if l := p.lines[k]; !l.blank && l.code && l.indent > indent {
			return l.indent
		}
	}
	return indent + 4
}

// docstring is the string a body opens with, its quotes and prefix removed.
func (p pyFile) docstring(from, end int) string {
	k := p.src.skip(from)
	if k >= end || p.src.m[k] != inString {
		return ""
	}
	e := k
	for e < end && p.src.m[e] == inString {
		e++
	}
	s := string(p.src.b[k:e])
	s = strings.TrimLeft(s, "rRbBuUfF")
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) && len(s) >= 2*len(q) {
			return strings.TrimSpace(s[len(q) : len(s)-len(q)])
		}
	}
	return strings.TrimSpace(s)
}

// lineOf is the index of the line holding offset off.
func lineOf(lines []pyLine, off int) int {
	lo, hi := 0, len(lines)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if lines[mid].start <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}
