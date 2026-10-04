package chunk

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/search"
)

func estimated(c search.Chunk) int { return Tokens([]byte(c.Breadcrumb + "\n" + c.Body)) }

func crumbsOf(chunks []search.Chunk) []string {
	var out []string
	for _, c := range chunks {
		out = append(out, c.Breadcrumb)
	}
	return out
}

func TestOversizedTableKeepsHeadersAndBoundedSourceSpans(t *testing.T) {
	var src strings.Builder
	src.WriteString("# Records\n\n| Link | Identifier |\n|---|---|\n")
	for i := range 98 {
		fmt.Fprintf(&src, "| https://example.org/%04d/%s | %s |\n", i, strings.Repeat("abcdef1234", 10), strings.Repeat("id", 50))
	}
	text := src.String()
	chunks := Markdown(markdown.Parse([]byte(text), markdown.GFM()), "Records", 0)
	if len(chunks) < 3 {
		t.Fatalf("oversized table produced %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if n := estimated(c); n > DefaultTokens {
			t.Errorf("chunk %d has %d estimated tokens (> %d)", c.Ord, n, DefaultTokens)
		}
		if c.ByteStart < 0 || c.ByteEnd > len(text) || c.ByteStart >= c.ByteEnd {
			t.Errorf("chunk %d has invalid source span %d:%d", c.Ord, c.ByteStart, c.ByteEnd)
		}
		if strings.Contains(c.Body, "example.org") && (!strings.Contains(c.Body, "| Link | Identifier |") || !strings.Contains(c.Body, "|---|---|")) {
			t.Errorf("chunk %d lost the table header: %q", c.Ord, c.Body[:min(80, len(c.Body))])
		}
	}
}

func TestTokenEstimateCountsUnbrokenURLs(t *testing.T) {
	s := "https://example.org/" + strings.Repeat("identifier0123456789", 250)
	if n := Tokens([]byte(s)); n < 1000 {
		t.Fatalf("long URL estimated at %d tokens", n)
	}
	if n := Tokens([]byte("one two three four five six seven eight nine ten")); n != 13 {
		t.Errorf("ten short words = %d tokens, want 13 (1.3 each)", n)
	}
}

func TestLongCodeAndListRespectSectionBudget(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"code", "```go\n" + strings.Repeat("fmt.Println(\"one two three four five six seven eight\")\n", 100) + "```\n"},
		{"list", "- root\n" + strings.Repeat("  - nested long item with the same many words and a URL https://example.org/abc123456789\n", 95)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "# Heading\n\n" + tc.body
			cs := Markdown(markdown.Parse([]byte(src), markdown.GFM()), "Heading", 128)
			if len(cs) < 2 {
				t.Fatalf("oversize %s did not split", tc.name)
			}
			for _, c := range cs {
				if got := estimated(c); got > 128 {
					t.Errorf("chunk %d estimated %d (>128)", c.Ord, got)
				}
				if tc.name == "code" && strings.Contains(c.Body, "fmt.Println") &&
					(!strings.HasPrefix(c.Body, "```go") || !strings.HasSuffix(strings.TrimSpace(c.Body), "```")) {
					t.Errorf("chunk %d lost fenced Go syntax: %q", c.Ord, c.Body[:min(60, len(c.Body))])
				}
			}
		})
	}
}

func TestLongTableHeaderCannotMakeAnOversizedChunk(t *testing.T) {
	text := "# Heading\n\n| " + strings.Repeat("headerid", 180) + " | Value |\n|---|---|\n" + strings.Repeat("| long row | value |\n", 40)
	for _, c := range Markdown(markdown.Parse([]byte(text), markdown.GFM()), "Heading", 128) {
		if n := estimated(c); n > 128 {
			t.Errorf("chunk %d has %d tokens", c.Ord, n)
		}
	}
}

func TestTitleOnlyAndHeaderOnlyTableStayBounded(t *testing.T) {
	title := strings.Repeat("long title with many words ", 80)
	cs := Markdown(markdown.Parse(nil, markdown.GFM()), title, 128)
	if len(cs) != 1 || cs[0].Body != "" || Tokens([]byte(cs[0].Breadcrumb)) > 128 {
		t.Fatalf("empty note with long title: %+v", cs)
	}
	table := "# Title\n\n| First | Second |\n|---|---|\n"
	cs = Markdown(markdown.Parse([]byte(table), markdown.GFM()), "Title", 128)
	if len(cs) != 1 || !strings.Contains(cs[0].Body, "| First | Second |") {
		t.Fatalf("header-only table: %+v", cs)
	}
}

// TestMarkdownSectionsAndBreadcrumbs: a section per heading, the title said once, definitions and
// frontmatter left out, ordinals from 0.
func TestMarkdownSectionsAndBreadcrumbs(t *testing.T) {
	src := "---\ntitle: Guide\n---\n\n# Guide\n\nIntro words.\n\n## Storage\n\nOne file.\n\n[r]: https://x\n\n### Durability\n\nThe log.\n"
	cs := Markdown(markdown.Parse([]byte(src), markdown.GFM(), markdown.Obsidian()), "Guide", 0)
	if got, want := crumbsOf(cs), []string{"Guide", "Guide > Storage", "Guide > Storage > Durability"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("breadcrumbs = %q, want %q", got, want)
	}
	for i, c := range cs {
		if c.Ord != i || strings.Contains(c.Body, "https://x") || strings.Contains(c.Body, "title:") {
			t.Errorf("chunk %d: %+v", i, c)
		}
		if got := src[c.ByteStart:c.ByteEnd]; !strings.Contains(got, strings.Fields(c.Body)[0]) {
			t.Errorf("chunk %d span %q does not hold its body %q", i, got, c.Body)
		}
	}
}

func TestPlainTextChunkSpansAndBound(t *testing.T) {
	src := []byte(strings.Repeat("long sentence with words\n", 100) + "\nsecond paragraph")
	chunks := Text(src, "notes", 64)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want split text", len(chunks))
	}
	for _, c := range chunks {
		if c.ByteStart < 0 || c.ByteEnd > len(src) || c.ByteStart >= c.ByteEnd {
			t.Fatalf("invalid source span: %+v", c)
		}
		if estimated(c) > 64 {
			t.Fatalf("oversized plain-text chunk: %+v", c)
		}
	}
	if cs := Text([]byte("  \n\n "), "empty", 0); len(cs) != 1 || cs[0].Body != "" || cs[0].Breadcrumb != "empty" {
		t.Errorf("a text with no words: %+v", cs)
	}
	// Markdown syntax in plain text is text
	cs := Text([]byte("# Puffin\n\n[[missing]] and #bird\n"), "notes", 0)
	if len(cs) != 1 || cs[0].Body != "# Puffin\n\n[[missing]] and #bird" {
		t.Errorf("plain text: %+v", cs)
	}
}

// One unit per top-level entry, its key in the breadcrumb, its scalars flattened with their key
// paths, spanning the entry's source.
func TestYAMLChunksByTopLevelEntry(t *testing.T) {
	src := "title: Service\nserver:\n  host: example\n  ports:\n    - 80\n    - 443\nowner: ops\n"
	meta, chunks := YAML([]byte(src), "svc.yaml", 512)
	if meta.Title != "Service" || meta.FrontmatterErr != "" {
		t.Fatalf("meta = %+v", meta)
	}
	if got, want := crumbsOf(chunks), []string{"Service > title", "Service > server", "Service > owner"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("units = %v, want %v", got, want)
	}
	server := chunks[1]
	if server.Body != "server > host: example\nserver > ports > [0]: 80\nserver > ports > [1]: 443" {
		t.Fatalf("server body = %q", server.Body)
	}
	if span := src[server.ByteStart:server.ByteEnd]; !strings.HasPrefix(span, "server:") || !strings.Contains(span, "443") || strings.Contains(span, "owner") {
		t.Fatalf("server span = %q: the whole entry and nothing more", span)
	}
}

// An entry over the limit is split along its lines, each part spanning its own values.
func TestYAMLOversizedEntrySplitsAlongLines(t *testing.T) {
	var b strings.Builder
	b.WriteString("big:\n")
	for i := range 200 {
		b.WriteString("  keyxxx" + strconv.Itoa(i) + ": value number " + strconv.Itoa(i) + " with several words in it\n")
	}
	src := b.String()
	_, chunks := YAML([]byte(src), "big.yaml", 128)
	if len(chunks) < 4 {
		t.Fatalf("%d chunks for an entry far over the limit", len(chunks))
	}
	prevEnd := 0
	for i, c := range chunks {
		if c.Breadcrumb != "big > big" {
			t.Fatalf("chunk %d breadcrumb = %q", i, c.Breadcrumb)
		}
		if estimated(c) > 128 {
			t.Fatalf("chunk %d is over the limit", i)
		}
		first := strings.SplitN(c.Body, "\n", 2)[0]
		value := first[strings.Index(first, ": ")+2:]
		if span := src[c.ByteStart:c.ByteEnd]; !strings.HasPrefix(span, value) || c.ByteStart < prevEnd {
			t.Fatalf("chunk %d span %q does not start at its first value %q (or overlaps)", i, span, value)
		}
		prevEnd = c.ByteEnd
	}
}

func TestYAMLSequenceAndMultiDocumentRoots(t *testing.T) {
	_, chunks := YAML([]byte("- name: a\n- name: b\n"), "list.yaml", 512)
	if got := crumbsOf(chunks); !reflect.DeepEqual(got, []string{"list > [0]", "list > [1]"}) {
		t.Fatalf("sequence units = %v", got)
	}
	_, chunks = YAML([]byte("a: 1\n---\nb: 2\n"), "two.yaml", 512)
	if got := crumbsOf(chunks); !reflect.DeepEqual(got, []string{"two > document 1 > a", "two > document 2 > b"}) {
		t.Fatalf("multi-document units = %v", got)
	}
}

func TestInvalidYAMLIsPlainText(t *testing.T) {
	meta, chunks := YAML([]byte("a: [unclosed\nflamingo\n"), "bad.yaml", 0)
	if meta.FrontmatterErr == "" || meta.Title != "bad" || len(chunks) != 1 || !strings.Contains(chunks[0].Body, "flamingo") {
		t.Fatalf("invalid YAML: %+v %+v", meta, chunks)
	}
}

func TestReadMeta(t *testing.T) {
	parse := func(s string) *markdown.Document {
		return markdown.Parse([]byte(s), markdown.GFM(), markdown.Obsidian())
	}
	m := ReadMeta(parse("---\ntitle: Guide\ntags: [Design, '#core']\naliases: arch\n---\n\n# Other\n\nText #Inline\n"), "x/guide.md")
	if m.Title != "Guide" || !reflect.DeepEqual(m.Tags, []string{"core", "design", "inline"}) || !reflect.DeepEqual(m.Aliases, []string{"arch"}) {
		t.Errorf("frontmatter: %+v", m)
	}
	if m.FrontmatterJSON != `{"title":"Guide","tags":["Design","#core"],"aliases":"arch"}` {
		t.Errorf("frontmatter JSON = %s", m.FrontmatterJSON)
	}
	if m := ReadMeta(parse("# Heading One\n\n## Two\n"), "x/n.md"); m.Title != "Heading One" {
		t.Errorf("the first h1: %+v", m)
	}
	if m := ReadMeta(parse("just text\n"), "x/name.md"); m.Title != "name" {
		t.Errorf("the file name: %+v", m)
	}
	if m := ReadMeta(parse("---\ntitle: [unclosed\n---\n\n# H\n"), "x/b.md"); m.FrontmatterErr == "" || m.Title != "H" {
		t.Errorf("broken frontmatter: %+v", m)
	}
	if m := ReadMeta(parse("---\n- a\n- b\n---\n\n# H\n"), "x/l.md"); m.FrontmatterErr != "the frontmatter is not a mapping" {
		t.Errorf("a list as frontmatter: %+v", m)
	}
}

func TestSnippet(t *testing.T) {
	if got := Snippet("short"); got != "short" {
		t.Errorf("short: %q", got)
	}
	long := strings.Repeat("é", 150) // 300 bytes, two per rune
	got := Snippet(long)
	if !strings.HasSuffix(got, "…") || len(got) > 200+len("…") || !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Errorf("a long body: %q (%d bytes)", got, len(got))
	}
	odd := "a" + strings.Repeat("é", 150) // the 200th byte is mid-rune
	if s := strings.TrimSuffix(Snippet(odd), "…"); len(s) != 199 {
		t.Errorf("cut mid-rune: kept %d bytes, want 199", len(s))
	}
}

// TestChunkers: the search.Chunker forms cut as the functions do, with their titles.
func TestChunkers(t *testing.T) {
	var r search.Chunkers
	for ext, c := range map[string]search.Chunker{".md": MarkdownChunker{}, ".txt": TextChunker{}, ".yaml": YAMLChunker{}} {
		if err := r.Register(ext, c); err != nil {
			t.Fatal(err)
		}
		if c.Version() != Version {
			t.Errorf("%s: version %q", ext, c.Version())
		}
	}
	md := []byte("---\ntitle: Guide\n---\n\n## Storage\n\nOne file.\n")
	c, _ := r.For("notes/guide.md")
	got, _ := c.Chunk(search.Doc{Path: "notes/guide.md", Text: md})
	if want := Markdown(markdown.Parse(md, markdown.GFM(), markdown.Obsidian()), "Guide", 0); !reflect.DeepEqual(got, want) {
		t.Errorf("Markdown: %+v, want %+v", got, want)
	}
	got, _ = c.Chunk(search.Doc{Path: "notes/guide.md", Title: "Named", Text: md, Tokens: 128})
	if got[0].Breadcrumb != "Named > Storage" {
		t.Errorf("a named title: %q", got[0].Breadcrumb)
	}
	c, _ = r.For("a/notes.txt")
	if got, _ := c.Chunk(search.Doc{Path: "a/notes.txt", Text: []byte("words")}); got[0].Breadcrumb != "notes" {
		t.Errorf("Text: %+v", got)
	}
	c, _ = r.For("svc.yaml")
	if got, _ := c.Chunk(search.Doc{Path: "svc.yaml", Title: "ignored", Text: []byte("a: 1\n")}); got[0].Breadcrumb != "svc > a" {
		t.Errorf("YAML: %+v", got)
	}
}
