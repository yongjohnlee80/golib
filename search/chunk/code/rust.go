package code

import (
	"strings"

	"github.com/yongjohnlee80/golib/search"
)

// Rust chunks Rust (.rs) by item: fn, struct, enum, union, trait, and impl blocks with their
// functions as units of their own, each with its doc comments (///, /** */) and attributes.
// use, mod, macros and other items fall into the header and glue chunks.
type Rust struct{}

const rsVersion = "code-rs-2"

func (Rust) Version() string { return rsVersion }

func (Rust) Chunk(d search.Doc) ([]search.Chunk, error) {
	t := newSrc(d.Text, rustSyntax())
	units, _ := rsItems(t, 0, len(t.b), d.Path, false)
	return assemble(d.Text, units, d.Path, d.Path+" > (glue)", d.Tokens), nil
}

func isRustDoc(c string) bool {
	return strings.HasPrefix(c, "///") || strings.HasPrefix(c, "/**")
}

// rsAttr skips one outer attribute at i (#[...]); an inner one (#![...]) belongs to the module,
// not to the item after it, and is not skipped.
func rsAttr(t src) func(int) int {
	return func(i int) int {
		if t.is(i, '#') && t.is(t.skipInline(i+1), '[') {
			return t.match(t.skipInline(i + 1))
		}
		return i
	}
}

// rsItems finds the items in [from, to). inImpl marks a trait or impl body, where only functions
// are units and anything else is a member's neighbour.
func rsItems(t src, from, to int, crumb string, inImpl bool) ([]unit, []bool) {
	var units []unit
	var isFn []bool
	i := from
	for i < to {
		start, doc, k := t.prefix(i, rsAttr(t), isRustDoc)
		if k >= to {
			break
		}
		if !t.code(k) {
			i = k + 1
			continue
		}
		sigStart := k
		w, e := t.word(k)
		// visibility and qualifiers before the keyword
		for {
			switch w {
			case "pub":
				k = t.skip(e)
				if t.is(k, '(') {
					k = t.skip(t.match(k))
				}
			case "async", "unsafe", "default", "const":
				nk := t.skip(e)
				if nw, _ := t.word(nk); w == "const" && nw != "fn" && nw != "unsafe" && nw != "async" && nw != "extern" {
					goto keyword // const ITEM: not a qualifier
				}
				k = nk
			case "extern":
				k = t.skip(e)
				if k < to && t.m[k] == inString { // the ABI: extern "C"
					for k < to && t.m[k] == inString {
						k++
					}
					k = t.skip(k)
				}
			default:
				goto keyword
			}
			w, e = t.word(k)
		}
	keyword:
		var us []unit
		end := -1
		fn := false
		switch w {
		case "fn":
			name, _ := t.word(t.skip(e))
			b := t.findCode(e, to, "{;")
			if b >= 0 && t.b[b] == '{' {
				end = t.match(b)
				us = []unit{braceUnit(t, crumb+" > "+name, start, sigStart, b, end, doc, "let", "fn")}
			} else if b >= 0 {
				end = b + 1
				us = []unit{{start: start, end: end, crumb: crumb + " > " + name,
					embed: joinNonEmpty(crumb+" > "+name, doc, string(t.b[sigStart:end]))}}
			}
			fn = true
		case "struct", "enum", "union":
			if inImpl {
				break
			}
			name, _ := t.word(t.skip(e))
			b := t.findCode(e, to, "{;")
			if b >= 0 {
				end = b + 1
				if t.b[b] == '{' {
					end = t.match(b)
				}
				ucrumb := crumb + " > " + name + " (" + w + ")"
				us = []unit{{start: start, end: end, crumb: ucrumb,
					embed: joinNonEmpty(ucrumb, doc, string(t.b[sigStart:end]))}}
			}
		case "trait", "impl":
			if inImpl {
				break
			}
			b := t.findCode(e, to, "{;")
			if b < 0 || t.b[b] != '{' {
				break
			}
			end = t.match(b)
			head := oneLine(string(t.b[sigStart:b]))
			ucrumb := crumb + " > " + rsHeadName(t, w, e, b)
			members, fns := rsItems(t, b+1, end-1, ucrumb, true)
			us = rsContainer(t, ucrumb, start, end, doc, head, members, fns)
		}
		if end > k && len(us) > 0 {
			units = append(units, us...)
			for range us {
				isFn = append(isFn, fn)
			}
			i = end
			continue
		}
		// not a unit: skip the item (a block, or up to its ";") into the header or glue
		next := t.findCode(k, to, ";{")
		switch {
		case next < 0:
			next = to
		case t.b[next] == '{':
			next = t.match(next)
			if t.is(t.skipInline(next), ';') {
				next = t.skipInline(next) + 1
			}
		default:
			next++
		}
		if next <= k {
			next = k + 1
		}
		i = next
	}
	return units, isFn
}

// rsHeadName names an impl or trait for breadcrumbs: "impl Display for Point", "trait Shape".
func rsHeadName(t src, kw string, e, b int) string {
	h := oneLine(string(t.b[e:b]))
	if i := strings.Index(h, " where "); i >= 0 {
		h = h[:i]
	}
	return kw + " " + strings.TrimSpace(h)
}

// rsContainer is a trait or impl: its head, each function, and what lies between functions
// (associated types and constants) as units of the block, the last taking the closing brace.
func rsContainer(t src, crumb string, start, end int, doc, head string, members []unit, fns []bool) []unit {
	var keep []unit
	for i, m := range members {
		if i < len(fns) && fns[i] {
			keep = append(keep, m)
		}
	}
	headEnd := end
	if len(keep) > 0 {
		headEnd = keep[0].start
	}
	units := []unit{{start: start, end: headEnd, crumb: crumb, tail: head, embed: joinNonEmpty(crumb, doc, head)}}
	for i, m := range keep {
		if i > 0 && hasCode(t, units[len(units)-1].end, m.start) {
			s, e := trimSpan(t.b, units[len(units)-1].end, m.start)
			units = append(units, unit{start: s, end: e, crumb: crumb})
		}
		units = append(units, m)
	}
	units[len(units)-1].end = end
	return units
}
