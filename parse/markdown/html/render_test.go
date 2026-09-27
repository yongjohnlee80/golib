package html_test

import (
	"bytes"
	"io/fs"
	"os"
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
