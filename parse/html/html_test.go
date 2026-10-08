package html

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func tokens(t *testing.T, src string) []Token {
	t.Helper()
	tz := NewTokenizer([]byte(src), Limits{})
	var out []Token
	for {
		tok, ok := tz.Next()
		if !ok {
			break
		}
		out = append(out, tok)
	}
	if err := tz.Err(); err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return out
}

func TestTokens(t *testing.T) {
	src := `<!DOCTYPE html><P Class="a &amp; b" id=x hidden data-x='q' class="dup">Fish &amp; chips<!-- c --><br/></P>`
	got := tokens(t, src)
	want := []struct {
		tok  Token
		text string // the token's span in the source
	}{
		{Token{Kind: Doctype, Data: "DOCTYPE html"}, "<!DOCTYPE html>"},
		{Token{Kind: StartTag, Name: "p", Attrs: []Attr{{"class", "a & b"}, {"id", "x"}, {"hidden", ""}, {"data-x", "q"}}}, `<P Class="a &amp; b" id=x hidden data-x='q' class="dup">`},
		{Token{Kind: Text, Data: "Fish & chips"}, "Fish &amp; chips"},
		{Token{Kind: Comment, Data: " c "}, "<!-- c -->"},
		{Token{Kind: SelfClosing, Name: "br"}, "<br/>"},
		{Token{Kind: EndTag, Name: "p"}, "</P>"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d tokens, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.tok.Kind || g.Name != w.tok.Name || g.Data != w.tok.Data || !slices.Equal(g.Attrs, w.tok.Attrs) {
			t.Errorf("token %d = %+v, want %+v", i, g, w.tok)
		}
		if span := src[g.Span[0]:g.Span[1]]; span != w.text {
			t.Errorf("token %d spans %q, want %q", i, span, w.text)
		}
	}
}

// A script's content is raw up to its own end tag, whatever it holds; a title's decodes entities.
func TestRawText(t *testing.T) {
	got := tokens(t, `<script>if (a < b && c) { s = "<div>"; }</SCRIPT><title>A &amp; B</title>`)
	if got[1].Kind != Text || got[1].Data != `if (a < b && c) { s = "<div>"; }` {
		t.Errorf("script content %+v", got[1])
	}
	if got[2].Kind != EndTag || got[2].Name != "script" {
		t.Errorf("script end %+v", got[2])
	}
	if got[4].Data != "A & B" {
		t.Errorf("title content %q", got[4].Data)
	}
	// "</script" inside a string ends the element, as it does in a browser.
	got = tokens(t, `<script>s = "</script>"; x</script>`)
	if got[1].Data != `s = "` {
		t.Errorf("script ends at its first end tag: %q", got[1].Data)
	}
}

// A '<' that starts no markup is text.
func TestStrayLessThan(t *testing.T) {
	got := tokens(t, `a < b <3 c`)
	if len(got) != 1 || got[0].Data != "a < b <3 c" {
		t.Errorf("%+v", got)
	}
}

func shape(n *Node) string {
	var b strings.Builder
	var walk func(n *Node)
	walk = func(n *Node) {
		switch n.Kind {
		case Text:
			b.WriteString(n.Data)
		case StartTag:
			if n.Name != "" {
				b.WriteString("(" + n.Name + " ")
			}
			for _, c := range n.Children {
				walk(c)
			}
			if n.Name != "" {
				b.WriteString(")")
			}
		}
	}
	walk(n)
	return b.String()
}

func TestImpliedEnds(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`<p>a<p>b`, `(p a)(p b)`},
		{`<p>a<div>b</div>`, `(p a)(div b)`},
		{`<ul><li>a<li>b<ul><li>c</ul></ul>`, `(ul (li a)(li b(ul (li c))))`},
		{`<dl><dt>t<dd>d<dt>u</dl>`, `(dl (dt t)(dd d)(dt u))`},
		{`<table><tr><td>1<td>2<tr><th>3</table>`, `(table (tr (td 1)(td 2))(tr (th 3)))`},
		{`<table><thead><tr><td>h<tbody><tr><td>b</table>`, `(table (thead (tr (td h)))(tbody (tr (td b))))`},
		{`<select><option>a<option>b</select>`, `(select (option a)(option b))`},
		{`<td><p>x<td>y`, `(td (p x))(td y)`},
		{`<b><i>x</b>y</i>`, `(b (i x))y`},
		{`<div>a</span>b</div>`, `(div ab)`},
		{`<img src=x>after<br>`, `(img )after(br )`},
	} {
		doc, err := Parse([]byte(c.src))
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		if got := shape(doc); got != c.want {
			t.Errorf("%q: %s, want %s", c.src, got, c.want)
		}
	}
}

func TestSpans(t *testing.T) {
	src := `<div><p>a</p><span>b`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	div := doc.Children[0]
	if div.Span != [2]int{0, len(src)} {
		t.Errorf("an unclosed element ends at the source's end: %v", div.Span)
	}
	if p := div.Children[0]; src[p.Span[0]:p.Span[1]] != "<p>a</p>" {
		t.Errorf("p's span %q", src[p.Span[0]:p.Span[1]])
	}
}

func TestLimitsRefuse(t *testing.T) {
	many := func(n int) string {
		var b strings.Builder
		b.WriteString("<div")
		for i := range n {
			b.WriteString(" a" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "=1")
		}
		return b.String() + " hidden>secret</div>"
	}
	for _, c := range []struct {
		name string
		src  string
		lim  Limits
	}{
		{"source", "<p>hello</p>", Limits{MaxSource: 5}},
		{"nodes", strings.Repeat("<b>x</b>", 100), Limits{MaxNodes: 50}},
		{"attribute count, hidden last", many(64), Limits{}},
		{"attribute value", `<div title="` + strings.Repeat("v", 64<<10+1) + `" hidden>secret</div>`, Limits{}},
	} {
		doc, err := ParseLimited(context.Background(), []byte(c.src), c.lim)
		if !errors.Is(err, ErrTooLarge) || doc != nil {
			t.Errorf("%s: %v, want ErrTooLarge and no tree", c.name, err)
		}
	}
	if _, err := ParseLimited(context.Background(), []byte(many(63)), Limits{}); err != nil {
		t.Errorf("64 attributes are within the limit: %v", err)
	}
}

// Past MaxDepth elements attach to the deepest allowed ancestor, and each node still knows the
// flattened elements around it: a noscript opened at depth 300 still encloses its text.
func TestFlattenedKeepsWhatEncloses(t *testing.T) {
	src := strings.Repeat("<div>", 300) + "<noscript>secret</noscript>after" + strings.Repeat("</div>", 300)
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// the deepest div with content: the last one the tree holds as an ancestor
	depth, n := 0, doc
	for len(n.Children) > 0 && n.Children[0].Name == "div" && len(n.Children[0].Children) > 0 {
		n, depth = n.Children[0], depth+1
	}
	if depth != DefaultLimits.MaxDepth {
		t.Fatalf("tree depth %d, want %d", depth, DefaultLimits.MaxDepth)
	}
	var secret, after *Node
	var find func(n *Node)
	find = func(n *Node) {
		for _, c := range n.Children {
			if c.Kind == Text && c.Data == "secret" {
				secret = c
			}
			if c.Kind == Text && c.Data == "after" {
				after = c
			}
		}
	}
	find(n)
	if secret == nil || after == nil {
		t.Fatalf("texts not under the deepest div: %s", shape(n))
	}
	var names []string
	for f := range secret.Flattened() {
		names = append(names, f.Name)
	}
	if len(names) == 0 || names[0] != "noscript" {
		t.Errorf("secret's flattened enclosers %v, want noscript first", names)
	}
	for f := range after.Flattened() {
		if f.Name == "noscript" {
			t.Error("after is outside the closed noscript")
		}
	}
}

func TestDeepNestingIsBounded(t *testing.T) {
	src := strings.Repeat("<i>", 10000) + "x" + strings.Repeat("</i>", 10000)
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	depth, n := 0, doc
	for len(n.Children) > 0 && n.Children[0].Kind == StartTag {
		n, depth = n.Children[0], depth+1
	}
	// MaxDepth open elements, then the flattened ones as childless leaves one level below
	if depth > DefaultLimits.MaxDepth+1 {
		t.Errorf("depth %d over the limit", depth)
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseLimited(ctx, []byte(strings.Repeat("<b>x</b>", 10000)), Limits{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v, want context.Canceled", err)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "<", "</", "</>", "<!", "<!--", "<a href=x>t</a>", `<div class="x`, "<script>", "<p>a<li>b<td>c",
		"<table><tr><td>1</table>", "&amp;&#x41;&bogus;", "<x a=\"1\" a='2' b c=d/>", "<?xml?><!DOCTYPE x>",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		doc, err := ParseLimited(context.Background(), src, Limits{MaxDepth: 16})
		if err != nil {
			return
		}
		stack := []*Node{doc}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n.Span[0] < 0 || n.Span[1] > len(src) || n.Span[0] > n.Span[1] {
				t.Fatalf("span %v outside a %d-byte source", n.Span, len(src))
			}
			stack = append(stack, n.Children...)
		}
	})
}

// Repeated attributes count against MaxAttrs as written: 65 copies of one name is over a limit
// of 64, and so is a list whose 65th attribute is the hidden that suppresses the element.
func TestDuplicateAttributesCountAgainstTheLimit(t *testing.T) {
	dup := "<p" + strings.Repeat(" a=1", 65) + ">x</p>"
	if _, err := ParseLimited(context.Background(), []byte(dup), Limits{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("65 repeats of one attribute: %v, want ErrTooLarge", err)
	}
	if _, err := ParseLimited(context.Background(), []byte("<p a=1 a=2>x</p>"), Limits{MaxAttrs: 1}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("two repeats over MaxAttrs 1: %v, want ErrTooLarge", err)
	}
	tk := NewTokenizer([]byte("<p a=1 a=2>"), Limits{MaxAttrs: 1})
	if _, ok := tk.Next(); ok || !errors.Is(tk.Err(), ErrTooLarge) {
		t.Errorf("tokenizer: ok %v err %v, want ErrTooLarge", ok, tk.Err())
	}
	if _, err := ParseLimited(context.Background(), []byte("<p a=1 a=2>x</p>"), Limits{MaxAttrs: 2}); err != nil {
		t.Errorf("two attributes within MaxAttrs 2: %v", err)
	}
}
