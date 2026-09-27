package markdown_test

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/parse/markdown/html"
)

func TestPositionCountsCommonMarkLines(t *testing.T) {
	src := []byte("a\r\nbc\rd\ne\xffx")
	d := markdown.Parse(src)
	for _, c := range []struct{ off, line, col int }{
		{0, 1, 1},  // a
		{3, 2, 1},  // b, after CRLF
		{4, 2, 2},  // c
		{6, 3, 1},  // d, after a bare CR: CommonMark's line, not parse.Scanner's
		{8, 4, 1},  // e
		{10, 4, 3}, // x: the invalid byte before it counts as one column
	} {
		p := d.Position(c.off)
		if p.Line != c.line || p.Column != c.col || p.Offset != c.off {
			t.Errorf("Position(%d) = %d:%d (offset %d), want %d:%d", c.off, p.Line, p.Column, p.Offset, c.line, c.col)
		}
	}
}

func find(n *markdown.Node, k markdown.Kind) *markdown.Node {
	if n.Kind == k {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.Next {
		if f := find(c, k); f != nil {
			return f
		}
	}
	return nil
}

func TestSpansCoverTheirConstructs(t *testing.T) {
	src := []byte("# h *x*\n\n> q `c`\n> r\n\n- [l](/u)\n\n[d]: /v\n")
	d := markdown.Parse(src)
	for _, c := range []struct {
		kind markdown.Kind
		want string
	}{
		{markdown.KindHeading, "# h *x*"},
		{markdown.KindEmph, "*x*"},
		{markdown.KindBlockQuote, "> q `c`\n> r"},
		{markdown.KindCodeSpan, "`c`"},
		{markdown.KindList, "- [l](/u)"},
		{markdown.KindLink, "[l](/u)"},
		{markdown.KindLinkRefDef, "[d]: /v"},
	} {
		n := find(d.Root, c.kind)
		if n == nil {
			t.Errorf("no %s node", c.kind)
			continue
		}
		if got := string(src[n.Span.Start:n.Span.End]); got != c.want {
			t.Errorf("%s spans %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestLiteralOnlyWhenTextDiffers(t *testing.T) {
	d := markdown.Parse([]byte("plain \\* &amp;\n"))
	for n := d.Root.FirstChild.FirstChild; n != nil; n = n.Next {
		if n.Kind != markdown.KindText {
			continue
		}
		verbatim := bytes.Equal(n.Text(d.Source), d.Source[n.Span.Start:n.Span.End])
		if n.Literal != nil && verbatim {
			t.Errorf("text %q carries a Literal equal to its source", n.Text(d.Source))
		}
	}
}

// checkTree asserts the structural invariants any input must keep: every span lies inside the
// source and inside its parent's, and siblings are ordered and do not overlap.
func checkTree(t *testing.T, src []byte, n *markdown.Node) {
	t.Helper()
	if n.Span.Start < 0 || n.Span.End > len(src) || n.Span.Start > n.Span.End {
		t.Fatalf("%s span %v outside source of %d bytes (input %q)", n.Kind, n.Span, len(src), src)
	}
	prevEnd := -1
	for c := n.FirstChild; c != nil; c = c.Next {
		if c.Parent != n {
			t.Fatalf("%s child %s has the wrong parent (input %q)", n.Kind, c.Kind, src)
		}
		if c.Span.Start < n.Span.Start || c.Span.End > n.Span.End {
			t.Fatalf("%s %v not inside parent %s %v (input %q)", c.Kind, c.Span, n.Kind, n.Span, src)
		}
		if c.Span.Start < prevEnd {
			t.Fatalf("%s %v overlaps its previous sibling ending at %d (input %q)", c.Kind, c.Span, prevEnd, src)
		}
		prevEnd = c.Span.End
		checkTree(t, src, c)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"", "# a\n", "> - *b*\n  c\n", "```\nx\n", "[a]: /u\n[a]", "<div>\n\n*x*",
		"a  \nb\\\nc", "***a**b*", "![x [y](/z)](/w)", "\t- \ta", "1. a\n\n\n2) b", "<a href='x'>y</a>",
		// shapes whose spans once overlapped: a block matched by a line that another block then took
		"a\n***\n", "a\n# h\n", "a\n```\nb\n", "a\n<div>\n", "- a\n\n[d]: /v\n", "    a\n    \nb\n",
		"*\n\n    - x\n", "> > >\n", ">\n> a\n", "- \n  a\n", "[a]: /u\n[b]: /v\n===\n", ">~~~",
		// GFM shapes
		"p\n| a | b |\n| - | - |\n| `c\\|` | d |\n", "> a\n> -:\n> b\n", "- [x] a\n  - [ ]\n", "~~a *b~~ c*",
		"(www.a.b/(c))) x@y.z. http://a.b?", "[x www.a.b](/u) a@b.c", "|\n-\n", "a|b\n-|-\n\tc"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		src := []byte(in)
		for _, opts := range [][]markdown.Option{nil, {markdown.GFM()}} {
			d := markdown.Parse(src, opts...)
			checkTree(t, src, d.Root)
			var b bytes.Buffer
			if err := html.Render(&b, d); err != nil {
				t.Fatal(err)
			}
		}
	})
}

// TestLinearTime measures each adversarial family at n and 10n. A quadratic parse would take about
// 100 times longer at 10n; the bound of 30 leaves room for timer and scheduler noise while still
// failing any super-linear family. Only the families measured here are claimed to be linear. The
// smaller input is large enough to leave the cache: below that, a linear parse still shows ratios
// near the bound, because the smaller run is the one that fits.
func TestLinearTime(t *testing.T) {
	if testing.Short() {
		t.Skip("timing measurement")
	}
	families := map[string]func(n int) string{
		"nested-open-brackets":  func(n int) string { return strings.Repeat("[", n) + "a" },
		"star-underscore-mod3":  func(n int) string { return strings.Repeat("*a**_b__", n/8) },
		"strong-emphasis-mixes": func(n int) string { return strings.Repeat("*a**b***", n/8) },
		"deep-block-quotes":     func(n int) string { return strings.Repeat(">", n) + " a" },
		"nested-lists": func(n int) string {
			var b strings.Builder
			for i := 0; i < n/40; i++ {
				b.WriteString(strings.Repeat("  ", i%20) + "- a\n")
			}
			return b.String()
		},
		"rising-backtick-runs": func(n int) string {
			var b strings.Builder
			for i := 1; b.Len() < n; i++ {
				b.WriteString(strings.Repeat("`", i%200+1) + "x")
			}
			return b.String()
		},
		"unclosed-link-dest":     func(n int) string { return strings.Repeat("[a](", n/4) },
		"many-shortcut-refs":     func(n int) string { return "[r]: /u\n\n" + strings.Repeat("[r] ", n/4) },
		"unmatched-closers":      func(n int) string { return strings.Repeat("a*", n/2) },
		"many-definitions":       func(n int) string { return strings.Repeat("[a]: /u\n", n/8) },
		"interrupted-paragraphs": func(n int) string { return strings.Repeat("a\n***\n", n/6) },
		"long-paragraph-lines":   func(n int) string { return strings.Repeat("word ", n/10) + "\n" + strings.Repeat("x\n", n/4) },
	}
	gfmFamilies := map[string]func(n int) string{
		"gfm-tilde-runs":      func(n int) string { return strings.Repeat("~~a", n/3) },
		"gfm-trailing-parens": func(n int) string { return "www.a.b/" + strings.Repeat(")", n) },
		"gfm-www-runs":        func(n int) string { return strings.Repeat("(www.a.b", n/8) },
		"gfm-at-signs":        func(n int) string { return strings.Repeat("a.b@", n/4) },
		"gfm-emails":          func(n int) string { return strings.Repeat("a@b.c ", n/6) },
		"gfm-table-rows":      func(n int) string { return "| a | b |\n| - | - |\n" + strings.Repeat("| c | `d\\|` |\n", n/14) },
		"gfm-wide-table": func(n int) string {
			h := strings.Repeat("a|", n/4)
			return h + "\n" + strings.Repeat("-|", n/4) + "\n" + h + "\n"
		},
		"gfm-task-items":       func(n int) string { return strings.Repeat("- [x] a\n", n/8) },
		"gfm-unclosed-bracket": func(n int) string { return "[" + strings.Repeat("www.a.b ", n/8) },
	}
	var opts []markdown.Option
	measure := func(src []byte) time.Duration {
		best := time.Duration(1<<63 - 1)
		for i := 0; i < 3; i++ {
			runtime.GC() // collect the previous run's garbage outside the timing
			start := time.Now()
			markdown.Parse(src, opts...)
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	const n = 50000
	check := func(name string, gen func(int) string) {
		small, large := []byte(gen(n)), []byte(gen(10*n))
		ts, tl := measure(small), measure(large)
		ratio := float64(tl) / float64(max(ts, time.Microsecond))
		t.Logf("%-22s %7d bytes %9v, %8d bytes %9v, ratio %.1f", name, len(small), ts, len(large), tl, ratio)
		if ratio > 30 {
			t.Errorf("%s: 10x the input took %.1fx the time — not linear", name, ratio)
		}
	}
	for name, gen := range families {
		check(name, gen)
	}
	opts = []markdown.Option{markdown.GFM()}
	for name, gen := range gfmFamilies {
		check(name, gen)
	}
	for name, gen := range families { // the CommonMark families stay linear with GFM on
		check(name+"+gfm", gen)
	}
}
