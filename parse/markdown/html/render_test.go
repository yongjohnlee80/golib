package html_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/parse/markdown/html"
)

// renderWith renders in with the given options; the conformance suite's render always uses Unsafe.
func renderWith(t *testing.T, in string, opts ...html.Option) string {
	t.Helper()
	var b bytes.Buffer
	if err := html.Render(&b, markdown.Parse([]byte(in)), opts...); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestSafeDefaultEscapesRawHTML renders the same inputs both ways: the default escapes raw HTML,
// Unsafe passes it through, and nothing else differs.
func TestSafeDefaultEscapesRawHTML(t *testing.T) {
	for _, c := range []struct{ in, safe, unsafe string }{
		{"<div>\n*a*\n</div>\n", "&lt;div&gt;\n*a*\n&lt;/div&gt;\n", "<div>\n*a*\n</div>\n"},
		{"a <b onclick=\"x\">c</b>\n", "<p>a &lt;b onclick=&quot;x&quot;&gt;c&lt;/b&gt;</p>\n", "<p>a <b onclick=\"x\">c</b></p>\n"},
		{"<!-- c -->\n", "&lt;!-- c --&gt;\n", "<!-- c -->\n"},
		{"*a* `<b>`\n", "<p><em>a</em> <code>&lt;b&gt;</code></p>\n", "<p><em>a</em> <code>&lt;b&gt;</code></p>\n"},
	} {
		if got := renderWith(t, c.in); got != c.safe {
			t.Errorf("default Render(%q) = %q, want %q", c.in, got, c.safe)
		}
		if got := renderWith(t, c.in, html.Unsafe()); got != c.unsafe {
			t.Errorf("Unsafe Render(%q) = %q, want %q", c.in, got, c.unsafe)
		}
	}
}

// TestInvalidUTF8 keeps invalid bytes in the tree, where a caller can see them, and renders each
// invalid sequence as U+FFFD.
func TestInvalidUTF8(t *testing.T) {
	d := markdown.Parse([]byte("a\xffb `c\xfed`\n"))
	para := d.Root.FirstChild
	if got := para.FirstChild.Text(d.Source); !bytes.Contains(got, []byte("\xff")) {
		t.Errorf("text %q lost its invalid byte", got)
	}
	code := para.LastChild
	if code.Kind != markdown.KindCodeSpan || !bytes.Contains(code.Text(d.Source), []byte("\xfe")) {
		t.Errorf("code span %s %q lost its invalid byte", code.Kind, code.Text(d.Source))
	}
	if got, want := renderWith(t, "a\xffb `c\xfed`\n"), "<p>a\uFFFDb <code>c\uFFFDd</code></p>\n"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// TestLargeRealNote parses and renders a note of more than 1 MB made of the module's own design
// documents, which use every common construct at realistic density.
func TestLargeRealNote(t *testing.T) {
	var note bytes.Buffer
	root := os.DirFS("../../../docs")
	err := fs.WalkDir(root, ".", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		b, err := fs.ReadFile(root, path)
		note.Write(b)
		note.WriteByte('\n')
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if note.Len() < 1<<20 {
		t.Fatalf("the module's docs total %d bytes, under 1 MB", note.Len())
	}
	d := markdown.Parse(note.Bytes())
	var out bytes.Buffer
	if err := html.Render(&out, d); err != nil {
		t.Fatal(err)
	}
	counts := map[markdown.Kind]int{}
	var walk func(n *markdown.Node)
	walk = func(n *markdown.Node) {
		counts[n.Kind]++
		for c := n.FirstChild; c != nil; c = c.Next {
			walk(c)
		}
	}
	walk(d.Root)
	for _, k := range []markdown.Kind{markdown.KindHeading, markdown.KindList, markdown.KindCodeBlock,
		markdown.KindLink, markdown.KindCodeSpan, markdown.KindEmph, markdown.KindStrong} {
		if counts[k] == 0 {
			t.Errorf("the note has no %s, so it does not exercise it", k)
		}
	}
	t.Logf("%d bytes in, %d bytes of HTML out, %d headings, %d code spans", note.Len(), out.Len(),
		counts[markdown.KindHeading], counts[markdown.KindCodeSpan])
}

// TestSafeDefaultURLs: by default a URL is written only if it has no scheme or an allowed one, so
// no document can link to script. Every path that writes an href or src is covered, and so are the
// ways a scheme can be spelled to slip past a naive check. Unsafe writes every URL.
func TestSafeDefaultURLs(t *testing.T) {
	for _, c := range []struct{ in, safe, unsafe string }{
		{"[a](javascript:alert(1))\n", `<p><a href="">a</a></p>` + "\n", `<p><a href="javascript:alert(1)">a</a></p>` + "\n"},
		{"[a](JaVaScRiPt:x) [b](vbscript:x) [c](file:///etc/passwd)\n",
			`<p><a href="">a</a> <a href="">b</a> <a href="">c</a></p>` + "\n",
			`<p><a href="JaVaScRiPt:x">a</a> <a href="vbscript:x">b</a> <a href="file:///etc/passwd">c</a></p>` + "\n"},
		// an entity reference or a backslash escape is decoded before the check, not after it
		{"[a](&#106;avascript:x) [b](javascript&colon;x) [c](java\\script:x)\n",
			`<p><a href="">a</a> <a href="">b</a> <a href="java%5Cscript:x">c</a></p>` + "\n",
			`<p><a href="javascript:x">a</a> <a href="javascript:x">b</a> <a href="java%5Cscript:x">c</a></p>` + "\n"},
		// whitespace is percent-encoded first, so it cannot split or precede a scheme
		{"[a](java&#9;script:x) [b](<\tjavascript:x>) [c](javascript%3Ax)\n",
			`<p><a href="java%09script:x">a</a> <a href="%09javascript:x">b</a> <a href="javascript%3Ax">c</a></p>` + "\n",
			`<p><a href="java%09script:x">a</a> <a href="%09javascript:x">b</a> <a href="javascript%3Ax">c</a></p>` + "\n"},
		{"<javascript:alert(1)>\n", `<p><a href="">javascript:alert(1)</a></p>` + "\n", `<p><a href="javascript:alert(1)">javascript:alert(1)</a></p>` + "\n"},
		{"![a](data:text/html,x) ![b](data:image/svg+xml,x) ![c](data:image/png;base64,AA) ![d](DATA:IMAGE/GIF,AA)\n",
			`<p><img src="" alt="a" /> <img src="" alt="b" /> <img src="data:image/png;base64,AA" alt="c" /> <img src="DATA:IMAGE/GIF,AA" alt="d" /></p>` + "\n",
			`<p><img src="data:text/html,x" alt="a" /> <img src="data:image/svg+xml,x" alt="b" /> <img src="data:image/png;base64,AA" alt="c" /> <img src="DATA:IMAGE/GIF,AA" alt="d" /></p>` + "\n"},
		{"![a](data:image/pngx,AA)\n", `<p><img src="" alt="a" /></p>` + "\n", `<p><img src="data:image/pngx,AA" alt="a" /></p>` + "\n"},
		{"[a](/p) [b](#f) [c](?q) [d](https://x.y) [e](mailto:a@b.c) [f](tel:1) [g](ftp://x.y) [h](a/b:c)\n",
			`<p><a href="/p">a</a> <a href="#f">b</a> <a href="?q">c</a> <a href="https://x.y">d</a> <a href="mailto:a@b.c">e</a> <a href="tel:1">f</a> <a href="ftp://x.y">g</a> <a href="a/b:c">h</a></p>` + "\n",
			`<p><a href="/p">a</a> <a href="#f">b</a> <a href="?q">c</a> <a href="https://x.y">d</a> <a href="mailto:a@b.c">e</a> <a href="tel:1">f</a> <a href="ftp://x.y">g</a> <a href="a/b:c">h</a></p>` + "\n"},
		{"[a]\n\n[a]: javascript:x\n", `<p><a href="">a</a></p>` + "\n", `<p><a href="javascript:x">a</a></p>` + "\n"},
	} {
		if got := renderWith(t, c.in); got != c.safe {
			t.Errorf("default Render(%q) =\n%q, want\n%q", c.in, got, c.safe)
		}
		if got := renderWith(t, c.in, html.Unsafe()); got != c.unsafe {
			t.Errorf("Unsafe Render(%q) =\n%q, want\n%q", c.in, got, c.unsafe)
		}
	}
	// the extensions' links go through the same writer
	for _, c := range []struct{ in, safe string }{
		{"[[javascript:alert(1)]] ![[javascript:x]]\n", `<p><a class="wikilink" href="">javascript:alert(1)</a> <span class="embed" data-href="">javascript:x</span></p>` + "\n"},
	} {
		var b bytes.Buffer
		if err := html.Render(&b, markdown.Parse([]byte(c.in), markdown.GFM(), markdown.Obsidian())); err != nil {
			t.Fatal(err)
		}
		if b.String() != c.safe {
			t.Errorf("default Render(%q) =\n%q, want\n%q", c.in, b.String(), c.safe)
		}
	}
}

// TestSourceSpans: with SourceSpans each block element carries its source bytes, which cover it;
// without it the output is unchanged, byte for byte, apart from those attributes.
func TestSourceSpans(t *testing.T) {
	src := "# Title\n\npara\n\n- item\n\n> quote\n\n```\ncode\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"
	doc := markdown.Parse([]byte(src), markdown.GFM())
	var plain, spans bytes.Buffer
	if err := html.Render(&plain, doc); err != nil {
		t.Fatal(err)
	}
	if err := html.Render(&spans, doc, html.SourceSpans()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "data-src") {
		t.Fatalf("data-src without SourceSpans:\n%s", plain.String())
	}
	got := spans.String()
	for tag, text := range map[string]string{"h1": "# Title", "p": "para", "li": "- item", "blockquote": "> quote", "pre": "```\ncode\n```", "table": "| a | b |"} {
		i := strings.Index(got, "<"+tag+` data-src="`)
		if i < 0 {
			t.Errorf("no <%s data-src> in\n%s", tag, got)
			continue
		}
		var from, to int
		if _, err := fmt.Sscanf(got[i+len(tag)+12:], "%d-%d", &from, &to); err != nil || from > to || to > len(src) {
			t.Errorf("<%s>'s data-src does not read: %v", tag, err)
			continue
		}
		if !strings.Contains(src[from:to], text) {
			t.Errorf("<%s data-src=%d-%d> covers %q, not %q", tag, from, to, src[from:to], text)
		}
	}
	stripped := regexp.MustCompile(` data-src="\d+-\d+"`).ReplaceAllString(got, "")
	if stripped != plain.String() {
		t.Errorf("SourceSpans changed more than its attributes:\n%s\nvs\n%s", stripped, plain.String())
	}
}
