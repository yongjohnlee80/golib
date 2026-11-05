package widget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/style"
)

// CODE BLOCK SYNTAX HIGHLIGHTING in the cells — the Editor's highlighter (package highlight), the
// language chosen by the block's language-* class, the colours the SAME SyntaxStyles a host gives
// its Editor (no second mapping: the page's code and the Raw view's agree); an unset style paints
// as the text's own, a language no definition has stays as it is.

// testCatalog is a three-word language: "kw" a keyword, "lit" a string, a /* comment carrying its
// state across lines.
func testCatalog() *highlight.Catalog {
	hl := highlight.HighlighterFunc(func(line string, st highlight.State) ([]highlight.Span, highlight.State) {
		if st == 1 {
			if end := strings.Index(line, "*/"); end >= 0 {
				return []highlight.Span{{Start: 0, End: end + 2, Style: highlight.Comment}}, 0
			}
			return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Comment}}, 1
		}
		var out []highlight.Span
		off, open := 0, strings.Index(line, "/*")
		if open >= 0 && strings.Index(line[open:], "*/") < 0 {
			out = append(out, highlight.Span{Start: open, End: len(line), Style: highlight.Comment})
			line, st = line[:open], 1
		}
		for len(line) > 0 {
			start, w := -1, -1
			var sty highlight.Style
			for _, m := range []struct {
				word string
				s    highlight.Style
			}{{"kw", highlight.Keyword}, {"lit", highlight.String}} {
				if i := strings.Index(line[off:], m.word); i >= 0 && (start < 0 || i < start) {
					start, w, sty = off+i, len(m.word), m.s
				}
			}
			if start < 0 {
				break
			}
			out = append(out, highlight.Span{Start: start, End: start + w, Style: sty})
			off = start + w
		}
		return out, st
	})
	return highlight.NewRepository(highlight.Definition{Name: "test", Aliases: []string{"tst"}, Highlighter: hl}).Snapshot()
}

// hostStyles is what a host gives its Editor and its page: keywords the accent, strings the
// success, comments the muted — the rest unset, painting as the text's own.
func hostStyles() SyntaxStyles {
	var st SyntaxStyles
	st[highlight.Keyword] = style.New().Foreground(style.TokenAccent)
	st[highlight.String] = style.New().Foreground(style.TokenSuccess)
	st[highlight.Comment] = style.New().Foreground(style.TokenTextMuted)
	return st
}

// syntaxGolden is src laid out in cells with the catalog and styles and painted, as a golden says
// it: each row's text, then its style runs.
func syntaxGolden(t *testing.T, html string, catalog *highlight.Catalog, styles SyntaxStyles) string {
	t.Helper()
	c := layCells(t, html, 40)
	c.syntax, c.syntaxStyles = catalog, styles
	c.layOut()
	s := newRecordSurface(c.width, len(c.rows))
	c.PaintCells(s, 0, c.width, DocPos{}, DocPos{})
	var b strings.Builder
	for y := range s.h {
		b.WriteString(goldenRow(s, y))
	}
	return b.String()
}

// goldenSyntax is got as the golden file says it, compared (or written with -update).
func goldenSyntax(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "htmlcells", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (go test ./tui/widget -run TestACodeBlock -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

const syntaxDoc = `<pre><code class="language-test">kw lit
/* a comment
still the comment */ lit
plain</code></pre>`

// A code block highlights in its language: its words in the styles' colours — the same
// SyntaxStyles the Editor paints with — a comment carried across lines by the highlighter's
// state, unmarked text the block's own.
func TestACodeBlockHighlightsInItsLanguage(t *testing.T) {
	got := syntaxGolden(t, syntaxDoc, testCatalog(), hostStyles())
	goldenSyntax(t, "syntax-language", got)
	if !strings.Contains(got, "fg=accent") {
		t.Error("no keyword colour: the host's styles map Keyword to the accent token")
	}
	if !strings.Contains(got, "fg=success") {
		t.Error("no string colour: the host's styles map String to the success token")
	}
}

// The host's styles are the only table: an entry a host leaves unset paints as the text's own,
// exactly as the Editor paints an unset style.
func TestAnUnsetStyleIsTheTextsOwn(t *testing.T) {
	var st SyntaxStyles
	st[highlight.Keyword] = style.New().Foreground(style.TokenError)
	got := syntaxGolden(t, syntaxDoc, testCatalog(), st)
	if !strings.Contains(got, "fg=error") {
		t.Error("no host colour: a keyword should wear the host's style, the error token here")
	}
	if strings.Contains(got, "fg=success") || strings.Contains(got, "fg=accent") {
		t.Errorf("an unset style painted anyway:\n%s", got)
	}
}

// Without a catalog there is no highlighting (the red-without-the-change check), a language no
// definition has stays as it is, and a block without a language too.
func TestNoCatalogNoDefinitionNoLanguageStaysPlain(t *testing.T) {
	plain := syntaxGolden(t, syntaxDoc, nil, hostStyles())
	if strings.Contains(plain, "fg=accent") || strings.Contains(plain, "fg=success") || strings.Contains(plain, "fg=error") {
		t.Errorf("without a catalog the block coloured:\n%s", plain)
	}
	unknown := syntaxGolden(t, `<pre><code class="language-nope">kw lit</code></pre>`, testCatalog(), hostStyles())
	if strings.Contains(unknown, "fg=accent") {
		t.Errorf("a language no definition has coloured:\n%s", unknown)
	}
	nolang := syntaxGolden(t, "<pre><code>kw lit</code></pre>", testCatalog(), hostStyles())
	if strings.Contains(nolang, "fg=accent") {
		t.Errorf("a block without a language coloured:\n%s", nolang)
	}
}

// The class sits on the <code> child of markdown's fences or on the <pre> itself, and an alias
// names the language as well: all three lay the same colours.
func TestTheClassAndItsAliasWhereverItSits(t *testing.T) {
	alias := `<pre><code class="language-tst">kw lit</code></pre>`
	onPre := `<pre class="language-test"><code>kw lit</code></pre>`
	want := syntaxGolden(t, `<pre><code class="language-test">kw lit</code></pre>`, testCatalog(), hostStyles())
	if got := syntaxGolden(t, alias, testCatalog(), hostStyles()); got != want {
		t.Errorf("an alias named it differently:\n%s\nwant\n%s", got, want)
	}
	if got := syntaxGolden(t, onPre, testCatalog(), hostStyles()); got != want {
		t.Errorf("the class on the pre itself named it differently:\n%s\nwant\n%s", got, want)
	}
}

// The language is read from the block, and the block keeps it: the walk's own record.
func TestTheBlockKeepsItsLanguage(t *testing.T) {
	c := layCells(t, syntaxDoc, 40)
	if len(c.doc) == 0 || c.doc[0].lang != "test" {
		t.Fatalf("the block's language %q", c.doc[0].lang)
	}
}

// The option applies when the view is made, before its document — a host's own order — and the
// document laid after it highlights: the cell layout reads the option's words at lay-out time,
// as WithDiagramNotice does.
func TestTheOptionAppliesBeforeTheDocument(t *testing.T) {
	v := NewHTMLView(WithSyntax(testCatalog(), hostStyles()))
	v.SetHTML([]byte(syntaxDoc))
	c := v.layout.(*cellHTML)
	s := newRecordSurface(c.width, len(c.rows))
	c.PaintCells(s, 0, c.width, DocPos{}, DocPos{})
	var b strings.Builder
	for y := range s.h {
		b.WriteString(goldenRow(s, y))
	}
	if !strings.Contains(b.String(), "fg=accent") {
		t.Errorf("a document laid into a view made with the option stayed plain:\n%s", b.String())
	}
}
