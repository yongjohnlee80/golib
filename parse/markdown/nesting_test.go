package markdown_test

import (
	"bytes"
	"io"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/parse/markdown/html"
)

// depth is how deep doc's tree runs below its root, walked without recursion.
func depth(doc *markdown.Document) int {
	type at struct {
		n *markdown.Node
		d int
	}
	most, stack := 0, []at{{doc.Root, 0}}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		most = max(most, x.d)
		for c := x.n.FirstChild; c != nil; c = c.Next {
			stack = append(stack, at{c, x.d + 1})
		}
	}
	return most
}

// count is how many nodes of kind doc holds, and text all of doc's text, in order.
func count(doc *markdown.Document, kind markdown.Kind) (n int, text string) {
	var b strings.Builder
	var walk func(*markdown.Node)
	walk = func(x *markdown.Node) {
		if x.Kind == kind {
			n++
		}
		if x.Kind == markdown.KindText {
			b.Write(x.Text(doc.Source))
		}
		for c := x.FirstChild; c != nil; c = c.Next {
			walk(c)
		}
	}
	walk(doc.Root)
	return n, b.String()
}

// TestMaxNestingKeepsTheExcessAsText: under MaxNesting(5) block quotes and list items open five
// containers deep and no deeper, emphasis and images nest five deep, and every marker past that is
// the deepest level's text; within the bound a document parses as it always did.
func TestMaxNestingKeepsTheExcessAsText(t *testing.T) {
	t.Parallel()
	five := markdown.MaxNesting(5)
	quotes := markdown.Parse([]byte(">>>>>>> a"), five)
	if n, text := count(quotes, markdown.KindBlockQuote); n != 5 || text != ">> a" {
		t.Errorf("seven '>' under MaxNesting(5): %d block quotes, text %q; want 5 and \">> a\"", n, text)
	}
	if n, _ := count(markdown.Parse([]byte(">>> a")), markdown.KindBlockQuote); n != 3 {
		t.Errorf("three '>', the default bound: %d block quotes, want 3", n)
	}
	lists := markdown.Parse([]byte("- - - - a"), five) // two items fit, a list and an item each
	if n, text := count(lists, markdown.KindItem); n != 2 || !strings.Contains(text, "- - a") {
		t.Errorf("four list markers under MaxNesting(5): %d items, text %q; want 2 and \"- - a\" kept", n, text)
	}
	emph := markdown.Parse([]byte(strings.Repeat("*", 7)+"a"+strings.Repeat("*", 7)), five)
	if d := depth(emph); d > 5+2 { // the paragraph, and the levels within it
		t.Errorf("seven '*' pairs under MaxNesting(5): depth %d, want at most 7", d)
	}
	// a start counted where it lands, not under the block it closes or the list it joins
	for src, want := range map[string]struct {
		kind markdown.Kind
		n    int
	}{
		">>>> a\n>>>>> b":                 {markdown.KindBlockQuote, 5}, // a paragraph interrupted at depth 4
		">>> a\n>>> - b":                  {markdown.KindList, 1},       // a list interrupting a paragraph at depth 3
		"> - - a\n>   - b":                {markdown.KindItem, 3},       // an item joining the deepest list
		">>>> | a |\n>>>> | - |\n>>>>> b": {markdown.KindBlockQuote, 5}, // a table at depth 5, closed by a quote
	} {
		if n, _ := count(markdown.Parse([]byte(src), five, markdown.WithGFM()), want.kind); n != want.n {
			t.Errorf("%q under MaxNesting(5): %d of kind %v, want %d", src, n, want.kind, want.n)
		}
	}
	images := markdown.Parse([]byte(strings.Repeat("![", 7)+"x"+strings.Repeat("](u)", 7)), five)
	if n, _ := count(images, markdown.KindImage); n != 5 {
		t.Errorf("seven nested images under MaxNesting(5): %d images, want 5", n)
	}
}

// TestADeepDocumentNestsNoDeeperThanTheBound: at the default bound, a thousand levels of each kind
// nest no deeper than MaxNesting (and one level over, for a link and the emphasis within it).
func TestADeepDocumentNestsNoDeeperThanTheBound(t *testing.T) {
	t.Parallel()
	const n = 1000
	for name, src := range map[string]string{
		"block quotes":               strings.Repeat(">", n) + " a",
		"list items":                 strings.Repeat("- ", n) + "a",
		"emphasis":                   strings.Repeat("*", n) + "a" + strings.Repeat("*", n),
		"strong":                     strings.Repeat("**", n) + "a" + strings.Repeat("**", n),
		"images":                     strings.Repeat("![", n) + "x" + strings.Repeat("](u)", n),
		"strikethrough and emphasis": strings.Repeat("~*", n) + "a" + strings.Repeat("*~", n),
	} {
		doc := markdown.Parse([]byte(src), markdown.WithGFM())
		if d := depth(doc); d > markdown.DefaultMaxNesting+3 {
			t.Errorf("%s, %d deep: the tree runs %d deep, want at most about %d", name, n, d, markdown.DefaultMaxNesting)
		}
	}
}

// TestADeepDocumentRendersOnASmallStack: a document 200,000 levels deep (block quotes, lists,
// emphasis) is parsed and rendered on an 8 MiB stack. Unbounded, the recursive walks over its tree
// overflow that, which is fatal: the process dies, not the test.
func TestADeepDocumentRendersOnASmallStack(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(8 << 20))
	const n = 200_000
	for _, src := range []string{
		strings.Repeat(">", n) + " a",
		strings.Repeat("- ", n) + "a",
		strings.Repeat("*", n) + "a" + strings.Repeat("*", n),
	} {
		doc := markdown.Parse([]byte(src), markdown.WithGFM())
		if err := html.Render(io.Discard, doc); err != nil {
			t.Fatalf("%.10q…: %v", src, err)
		}
		var b bytes.Buffer
		_ = html.Render(&b, doc)
		if b.Len() == 0 {
			t.Errorf("%.10q…: rendered nothing", src)
		}
	}
}
