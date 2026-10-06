package chunk

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yongjohnlee80/golib/parse/markdown"
	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
	"github.com/yongjohnlee80/golib/search"
)

// Relation is one value of a relation field in a document's frontmatter, as written: a field such
// as supersedes or related whose values name other documents.
type Relation struct {
	Field string // the field it is listed under
	// Raw is the value's text. An unquoted [[x]], which YAML reads as a list holding a list, comes
	// back as "[[x]]", as it was written.
	Raw  string
	Line int // its line in the document, from 1
}

// Relations lists the values of the named fields in a parsed note's frontmatter, in source order.
// A field holding one scalar gives one value, and a list gives one per item. YAML reads an unquoted
// [[x]] as a list holding a list holding x; that exact shape, as the field's value or as an item,
// gives "[[x]]". Any other nested list, a mapping, and a frontmatter that does not parse give
// nothing: no value is made up from structure that was not written as one.
func Relations(doc *markdown.Document, fields []string) []Relation {
	fm, off := frontmatter(doc)
	if fm == nil {
		return nil
	}
	root := frontmatterRoot(fm.Literal)
	if root == nil || root.Kind != pyaml.KindMapping {
		return nil
	}
	want := map[string]bool{}
	for _, f := range fields {
		want[f] = true
	}
	var out []Relation
	for _, p := range root.Pairs {
		if p.Key == nil || p.Key.Kind != pyaml.KindScalar || !want[string(p.Key.Value)] || p.Value == nil {
			continue
		}
		field := string(p.Key.Value)
		add := func(raw string, n *pyaml.Node) {
			if raw = strings.TrimSpace(raw); raw != "" {
				out = append(out, Relation{Field: field, Raw: raw, Line: lineAt(doc.Source, off+n.Span.Start)})
			}
		}
		v := p.Value
		if s := unquotedWikilink(v); s != nil {
			add("[["+string(s.Value)+"]]", s)
			continue
		}
		switch v.Kind {
		case pyaml.KindScalar:
			add(string(v.Value), v)
		case pyaml.KindSequence:
			for _, item := range v.Items {
				if item.Kind == pyaml.KindScalar {
					add(string(item.Value), item)
				} else if s := unquotedWikilink(item); s != nil {
					add("[["+string(s.Value)+"]]", s)
				}
			}
		}
	}
	return out
}

// unquotedWikilink is x when n is how YAML reads an unquoted [[x]]: a list holding exactly one list
// holding exactly one scalar. Anything else is nil.
func unquotedWikilink(n *pyaml.Node) *pyaml.Node {
	if n == nil || n.Kind != pyaml.KindSequence || len(n.Items) != 1 {
		return nil
	}
	inner := n.Items[0]
	if inner.Kind != pyaml.KindSequence || len(inner.Items) != 1 || inner.Items[0].Kind != pyaml.KindScalar {
		return nil
	}
	return inner.Items[0]
}

// RefForm is what a relation value names, and so how it is looked up.
type RefForm uint8

const (
	// RefProse is text that names no document: a note on where something came from.
	RefProse RefForm = iota
	// RefPath is a path, root-relative or relative to the document: "adrs/0001.md".
	RefPath
	// RefWikilink is a [[wikilink]]: Target is its page, Anchor its heading.
	RefWikilink
	// RefSlug is a bare name with no spaces or slashes, looked up as a wikilink is: "golib-vfs-0001".
	RefSlug
	// RefNumber is digits alone, a decision record's number: "0021".
	RefNumber
	// RefURL is an address with a scheme: "https://example.com/x".
	RefURL
)

// Ref is a relation value read for lookup.
type Ref struct {
	Form RefForm
	// Target is what to look up: the path without its root prefix, the wikilink's page, the slug or
	// the number. For a URL it is the URL, and for prose the text.
	Target string
	// Anchor is a section named after the target: a section sign and number after a path ("§N.N"),
	// or "#heading" in a wikilink.
	Anchor string
	// Note is the annotation after the target that is not an anchor: "rev N", "+ rationale".
	Note string
}

var (
	refScheme   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://\S+$`)
	refRoot     = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*/`)
	refNumber   = regexp.MustCompile(`^[0-9]+$`)
	refSlug     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	refWikilink = regexp.MustCompile(`^\[\[([^\[\]]+)\]\]$`)
	// an annotation after a target, one or more: "§N.N", "rev N", or in parentheses "(rev N)" or a
	// "(+ …)" addition; any other parenthesis is prose
	refAnnot = regexp.MustCompile(`^(?:§\s*\S+|\(rev\s+[^()\s]+\)|\(\+[^()]*\)|rev\s+\S+)(?:\s+(?:§\s*\S+|\(rev\s+[^()\s]+\)|\(\+[^()]*\)|rev\s+\S+))*$`)
)

// ParseRef reads a relation value. A path loses a leading "$NAME/" root prefix ("$KB_ROOT/a.md"
// is "a.md", root-relative). A target may be followed by annotations, which come off: a section
// sign and number ("§N.N") becomes the Anchor, and "(rev N)", "rev N" or "(+ rationale)" the Note. Text after a target
// that is not such an annotation makes the whole value prose: "Johno 2026-10-05 (chat): …" names
// no document.
func ParseRef(raw string) Ref {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Ref{Form: RefProse}
	}
	if m := refWikilink.FindStringSubmatch(s); m != nil {
		page := m[1]
		if i := strings.IndexByte(page, '|'); i >= 0 {
			page = page[:i]
		}
		var anchor string
		if i := strings.IndexByte(page, '#'); i >= 0 {
			page, anchor = page[:i], page[i+1:]
		}
		return Ref{Form: RefWikilink, Target: strings.TrimSpace(page), Anchor: strings.TrimSpace(anchor)}
	}
	if refScheme.MatchString(s) {
		return Ref{Form: RefURL, Target: s}
	}
	target, rest, _ := strings.Cut(s, " ")
	rest = strings.TrimSpace(rest)
	var anchor, note string
	if rest != "" {
		if !refAnnot.MatchString(rest) {
			return Ref{Form: RefProse, Target: s}
		}
		anchor, note = annotations(rest)
	}
	target = refRoot.ReplaceAllString(target, "")
	ref := Ref{Target: target, Anchor: anchor, Note: note}
	switch {
	case refNumber.MatchString(target):
		ref.Form = RefNumber
	case strings.ContainsRune(target, '/') || strings.HasSuffix(target, ".md"):
		ref.Form = RefPath
	case refSlug.MatchString(target):
		ref.Form = RefSlug
	default:
		return Ref{Form: RefProse, Target: s}
	}
	return ref
}

// annotations splits "§N.N (rev N)" into its anchor and its note.
func annotations(s string) (anchor, note string) {
	var notes []string
	for s != "" {
		switch {
		case strings.HasPrefix(s, "§"):
			tok := strings.TrimSpace(strings.TrimPrefix(s, "§"))
			anchor, s, _ = strings.Cut(tok, " ")
		case strings.HasPrefix(s, "("):
			end := strings.IndexByte(s, ')')
			notes = append(notes, strings.TrimSpace(s[1:end]))
			s = s[end+1:]
		default: // "rev N"
			fields := strings.Fields(s)
			notes = append(notes, fields[0]+" "+fields[1])
			s = strings.Join(fields[2:], " ")
		}
		s = strings.TrimSpace(s)
	}
	return anchor, strings.Join(notes, "; ")
}

// Abstract is the chunk of a document's abstract, from its frontmatter's abstract field
// ([Meta].Abstract): breadcrumb "title > abstract", the abstract as its body, and the abstract
// value's span in the source. ok is false when the document has none. The caller numbers it (Ord):
// placed after the document's other chunks, it moves none of them.
func Abstract(m Meta, title string) (c search.Chunk, ok bool) {
	if strings.TrimSpace(m.Abstract) == "" {
		return search.Chunk{}, false
	}
	return search.Chunk{Breadcrumb: title + " > abstract", Body: m.Abstract,
		ByteStart: m.AbstractStart, ByteEnd: m.AbstractEnd}, true
}

// frontmatter is the note's frontmatter node and where its literal starts in the source: after
// the opening "---" line.
func frontmatter(doc *markdown.Document) (*markdown.Node, int) {
	fm := doc.Root.FirstChild
	if fm == nil || fm.Kind != markdown.KindFrontmatter {
		return nil, 0
	}
	return fm, bytes.IndexByte(doc.Source, '\n') + 1
}

// frontmatterRoot is the root node of a frontmatter's one YAML document, or nil.
func frontmatterRoot(raw []byte) *pyaml.Node {
	st, err := pyaml.Parse(raw)
	if err != nil || len(st.Docs) != 1 {
		return nil
	}
	return st.Docs[0].Root
}

// lineAt is the line, from 1, holding byte off of src.
func lineAt(src []byte, off int) int {
	return bytes.Count(src[:min(max(off, 0), len(src))], []byte{'\n'}) + 1
}
