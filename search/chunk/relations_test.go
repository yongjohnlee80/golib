package chunk

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
)

func note(src string) *markdown.Document {
	return markdown.Parse([]byte(src), markdown.GFM(), markdown.Obsidian())
}

var relationFields = []string{"supersedes", "superseded_by", "amends", "related", "sources", "adr"}

func TestRelationsReadsEachFormYAMLGives(t *testing.T) {
	t.Parallel()
	src := `---
title: A decision
supersedes: 0019
amends:
  - adrs/0064-wire.md §2.3
related: [[golib-vfs-0001]]
sources:
- [[notes/one]]
- "Johno 2026-10-05 (chat): keep it simple"
- {not: a value}
adr: [21, "$KB_ROOT/adrs/0084-x.md"]
tags: [ignored, here]
---
# Body
`
	// sources' items sit at column 0, their values at column 2: a line read from the frontmatter's
	// own offsets, not the document's, lands on the line above
	got := Relations(note(src), relationFields)
	want := []Relation{
		{"supersedes", "0019", 3},
		{"amends", "adrs/0064-wire.md §2.3", 5},
		{"related", "[[golib-vfs-0001]]", 6},
		{"sources", "[[notes/one]]", 8},
		{"sources", "Johno 2026-10-05 (chat): keep it simple", 9},
		{"adr", "21", 11},
		{"adr", "$KB_ROOT/adrs/0084-x.md", 11},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Relations =\n%v\nwant\n%v", got, want)
	}
}

// TestRelationsMakeNoWikilinkFromOtherNestedLists: only the exact shape YAML reads an unquoted [[x]]
// as gives "[[x]]"; a nested list of two, or one nested deeper, was not written as a wikilink.
func TestRelationsMakeNoWikilinkFromOtherNestedLists(t *testing.T) {
	t.Parallel()
	src := "---\nrelated: [[foo, bar]]\nsources:\n- [foo, bar]\n- [[[deep]]]\n- plain\n---\nbody\n"
	got := Relations(note(src), relationFields)
	want := []Relation{{"sources", "plain", 6}}
	if !slices.Equal(got, want) {
		t.Errorf("Relations = %v, want only %v", got, want)
	}
}

func TestRelationsWithoutFrontmatter(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"no frontmatter":         "# Title\n\nbody\n",
		"frontmatter not YAML":   "---\nrelated: [unclosed\n---\nbody\n",
		"frontmatter a sequence": "---\n- a\n- b\n---\nbody\n",
		"no field asked for":     "---\ntitle: x\n---\nbody\n",
	} {
		if got := Relations(note(src), relationFields); len(got) != 0 {
			t.Errorf("%s: Relations = %v, want none", name, got)
		}
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		raw  string
		want Ref
	}{
		{"adrs/0075-front-door.md", Ref{Form: RefPath, Target: "adrs/0075-front-door.md"}},
		{"$KB_ROOT/adrs/0084-x.md", Ref{Form: RefPath, Target: "adrs/0084-x.md"}},
		{"adrs/0064-wire.md §2.3", Ref{Form: RefPath, Target: "adrs/0064-wire.md", Anchor: "2.3"}},
		{"conventions/vocabulary.md rev 3", Ref{Form: RefPath, Target: "conventions/vocabulary.md", Note: "rev 3"}},
		{"adrs/golib-vfs-0002.md (+ rationale)", Ref{Form: RefPath, Target: "adrs/golib-vfs-0002.md", Note: "+ rationale"}},
		{"adrs/0061-x.md §2.8 (rev 2)", Ref{Form: RefPath, Target: "adrs/0061-x.md", Anchor: "2.8", Note: "rev 2"}},
		{"notes.md", Ref{Form: RefPath, Target: "notes.md"}},
		{"/home/someone/golib/docs/adr.md", Ref{Form: RefPath, Target: "/home/someone/golib/docs/adr.md"}},
		{"[[0075-front-door]]", Ref{Form: RefWikilink, Target: "0075-front-door"}},
		{"[[notes/a#Heading|shown]]", Ref{Form: RefWikilink, Target: "notes/a", Anchor: "Heading"}},
		{"golib-vfs-0001", Ref{Form: RefSlug, Target: "golib-vfs-0001"}},
		{"2026-09-02-autodb-pr44-review", Ref{Form: RefSlug, Target: "2026-09-02-autodb-pr44-review"}},
		{"0209", Ref{Form: RefNumber, Target: "0209"}},
		{"21", Ref{Form: RefNumber, Target: "21"}},
		{"https://doc.qt.io/qt-6/qtqml.html", Ref{Form: RefURL, Target: "https://doc.qt.io/qt-6/qtqml.html"}},
		{"Johno 2026-10-05 (chat): keep it simple", Ref{Form: RefProse, Target: "Johno 2026-10-05 (chat): keep it simple"}},
		{"autodb/tui-qml branch at aab0f1e", Ref{Form: RefProse, Target: "autodb/tui-qml branch at aab0f1e"}},
		{"mailbox:1790492671-0001-review", Ref{Form: RefProse, Target: "mailbox:1790492671-0001-review"}},
		{"measured 31 times", Ref{Form: RefProse, Target: "measured 31 times"}},
		// a parenthesis is an annotation only as (rev N) or a (+ …) addition: anything else is prose
		{"release (discussion)", Ref{Form: RefProse, Target: "release (discussion)"}},
		{"adrs/x.md (see the thread)", Ref{Form: RefProse, Target: "adrs/x.md (see the thread)"}},
		{"release ()", Ref{Form: RefProse, Target: "release ()"}},
		{"golib-vfs-0001 (rev 2)", Ref{Form: RefSlug, Target: "golib-vfs-0001", Note: "rev 2"}},
		{"adrs/x.md rev 3.1", Ref{Form: RefPath, Target: "adrs/x.md", Note: "rev 3.1"}},
		// a revision is a number: "rev" and a word is prose
		{"release (rev discussion)", Ref{Form: RefProse, Target: "release (rev discussion)"}},
		{"release rev discussion", Ref{Form: RefProse, Target: "release rev discussion"}},
		{"  ", Ref{Form: RefProse}},
	} {
		if got := ParseRef(c.raw); got != c.want {
			t.Errorf("ParseRef(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}

func TestMetaReadsTheAbstractAndItsSpan(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src, want, span string }{
		{"plain", "---\ntitle: T\nabstract: The short summary.\n---\n# T\n", "The short summary.", "The short summary."},
		{"quoted", "---\nabstract: \"A: quoted one\"\n---\nbody\n", "A: quoted one", `"A: quoted one"`},
		{"folded", "---\nabstract: >\n  First line\n  second line\n---\nbody\n", "First line second line", ">\n  First line\n  second line\n"},
	} {
		doc := note(c.src)
		m := ReadMeta(doc, "x.md")
		if m.Abstract != c.want {
			t.Errorf("%s: Abstract = %q, want %q", c.name, m.Abstract, c.want)
		}
		if got := c.src[m.AbstractStart:m.AbstractEnd]; got != c.span {
			t.Errorf("%s: the span is %q, want %q", c.name, got, c.span)
		}
	}
	if m := ReadMeta(note("---\ntitle: T\n---\nbody\n"), "x.md"); m.Abstract != "" || m.AbstractStart != 0 || m.AbstractEnd != 0 {
		t.Errorf("no abstract: %+v", m)
	}
}

func TestAbstractChunk(t *testing.T) {
	t.Parallel()
	src := "---\ntitle: Storage\nabstract: Where the index keeps its rows.\n---\n# Storage\n\nThe body.\n"
	doc := note(src)
	m := ReadMeta(doc, "storage.md")
	c, ok := Abstract(m, m.Title)
	if !ok {
		t.Fatal("no abstract chunk for a note with an abstract")
	}
	if c.Breadcrumb != "Storage > abstract" || c.Body != "Where the index keeps its rows." {
		t.Errorf("chunk = %+v", c)
	}
	if got := src[c.ByteStart:c.ByteEnd]; got != "Where the index keeps its rows." {
		t.Errorf("the chunk's span is %q, not the abstract", got)
	}
	if _, ok := Abstract(ReadMeta(note("# Plain\n"), "plain.md"), "Plain"); ok {
		t.Error("an abstract chunk for a note with none")
	}
}

// TestTheAbstractLeavesTheMarkdownChunksAlone: the Markdown chunker reads no frontmatter, so a
// note with an abstract chunks exactly as without one, and its chunks' identities do not move.
func TestTheAbstractLeavesTheMarkdownChunksAlone(t *testing.T) {
	t.Parallel()
	body := "# Storage\n\nThe body.\n\n## Rows\n\nRows are kept.\n"
	with := Markdown(note("---\nabstract: A summary.\n---\n"+body), "Storage", 0)
	without := Markdown(note("---\ntitle: Storage\n---\n"+body), "Storage", 0)
	if len(with) != len(without) {
		t.Fatalf("%d chunks with an abstract, %d without", len(with), len(without))
	}
	for i := range with {
		if with[i].Breadcrumb != without[i].Breadcrumb || with[i].Body != without[i].Body || with[i].Ord != i {
			t.Errorf("chunk %d moved: %+v vs %+v", i, with[i], without[i])
		}
	}
	if with[0].Ord != 0 {
		t.Errorf("ord 0 is %d", with[0].Ord)
	}
	if Version != "5" {
		t.Errorf("chunk.Version is %q: a new version re-embeds every chunk of every index", Version)
	}
}
