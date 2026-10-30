package markdown_test

import (
	"bytes"
	"runtime"
	"slices"
	"sort"
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
		"(www.a.b/(c))) x@y.z. http://a.b?", "[x www.a.b](/u) a@b.c", "|\n-\n", "a|b\n-|-\n\tc",
		// Obsidian shapes
		"---\na: 1\n---\n#t [[P#H|a]] ![[i.png]]", "---\r\n---", "> [!x]- T\n> [[a\n> b]]", "[x [[P]]](/u)",
		"| [[a\\|b]] |\n| - |", "#1 #a1 [[[[b]] ]]", "> [!note]\n>> [!q]"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		src := []byte(in)
		for _, opts := range [][]markdown.Option{nil, {markdown.WithGFM()}, {markdown.WithObsidian()}, {markdown.WithGFM(), markdown.WithObsidian()}} {
			d := markdown.Parse(src, opts...)
			checkTree(t, src, d.Root)
			var b bytes.Buffer
			if err := html.Render(&b, d); err != nil {
				t.Fatal(err)
			}
		}
	})
}

// linearFamily is one adversarial input shape, with the extensions it is parsed under.
type linearFamily struct {
	name string
	opts []markdown.Option
	gen  func(n int) string // about n bytes
}

// linearFamilies are the shapes TestLinearTime and BenchmarkParseFamilies measure: the CommonMark
// ones alone, with GFM and with GFM and Obsidian on, and the GFM and Obsidian ones under their
// extensions. Only these are claimed to parse in linear time.
func linearFamilies() []linearFamily {
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
		// raw HTML openers whose closer never comes: each search is the paragraph's one
		"unclosed-html-comments": func(n int) string { return strings.Repeat("a <!--", n/6) },
		"unclosed-html-pis":      func(n int) string { return strings.Repeat("a <?", n/4) },
		"unclosed-html-cdata":    func(n int) string { return strings.Repeat("a <![CDATA[", n/11) },
		"unclosed-html-decls":    func(n int) string { return strings.Repeat("a <!A", n/5) },
		// open brackets under many links: each link deactivates the openers below it once
		"open-brackets-then-links": func(n int) string { return strings.Repeat("[", n/2) + strings.Repeat("[a](b) ", n/14) },
		// open image openers under many links: no link passes the image openers again
		"open-images-then-links": func(n int) string { return strings.Repeat("![", n/4) + strings.Repeat("[a](b) ", n/14) },
	}
	gfmFamilies := map[string]func(n int) string{
		"gfm-tilde-runs":      func(n int) string { return strings.Repeat("~~a", n/3) },
		"gfm-trailing-parens": func(n int) string { return "www.a.b/" + strings.Repeat(")", n) },
		"gfm-www-runs":        func(n int) string { return strings.Repeat("(www.a.b", n/8) },
		"gfm-at-signs":        func(n int) string { return strings.Repeat("a.b@", n/4) },
		"gfm-emails":          func(n int) string { return strings.Repeat("a@b.c ", n/6) },
		// a paragraph of many lines, its addresses each checked against the line starts
		"gfm-email-lines":       func(n int) string { return strings.Repeat("a@b.c\n", n/6) },
		"gfm-email-plus-chains": func(n int) string { return strings.Repeat("x\n", n/4) + strings.Repeat("a@b.c+", n/12) },
		"gfm-table-rows":        func(n int) string { return "| a | b |\n| - | - |\n" + strings.Repeat("| c | `d\\|` |\n", n/14) },
		"gfm-wide-table": func(n int) string {
			h := strings.Repeat("a|", n/4)
			return h + "\n" + strings.Repeat("-|", n/4) + "\n" + h + "\n"
		},
		"gfm-task-items":       func(n int) string { return strings.Repeat("- [x] a\n", n/8) },
		"gfm-unclosed-bracket": func(n int) string { return "[" + strings.Repeat("www.a.b ", n/8) },
	}
	obsidianFamilies := map[string]func(n int) string{
		"obs-open-brackets-then-wikilinks": func(n int) string { return strings.Repeat("[", n/2) + strings.Repeat("[[a]] ", n/12) },
		"obs-unclosed-wikilinks":           func(n int) string { return strings.Repeat("[[a", n/3) },
		"obs-nested-openers":               func(n int) string { return strings.Repeat("[[a", n/6) + strings.Repeat("]]", n/6) },
		"obs-wikilinks":                    func(n int) string { return strings.Repeat("[[P#H|a]] ", n/10) },
		"obs-embeds-in-brackets":           func(n int) string { return strings.Repeat("[x ![[i]] ", n/10) },
		"obs-tags":                         func(n int) string { return strings.Repeat("#t/a-b ", n/7) },
		"obs-digit-tags":                   func(n int) string { return strings.Repeat("#123 ", n/5) },
		"obs-callouts":                     func(n int) string { return strings.Repeat("> [!note]- T\n> a\n\n", n/18) },
		"obs-unclosed-front":               func(n int) string { return "---\n" + strings.Repeat("a: b\n", n/5) },
	}
	var out []linearFamily
	add := func(suffix string, opts []markdown.Option, m map[string]func(int) string) {
		for name, gen := range m {
			out = append(out, linearFamily{name + suffix, opts, gen})
		}
	}
	gfm := []markdown.Option{markdown.WithGFM()}
	both := []markdown.Option{markdown.WithGFM(), markdown.WithObsidian()}
	add("", nil, families)
	add("", gfm, gfmFamilies)
	add("+gfm", gfm, families) // the CommonMark families stay linear with GFM on
	add("", both, obsidianFamilies)
	add("+gfm+obs", both, families)
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// TestLinearTime measures each adversarial family at n and 4n. Both sizes are past the caches: below
// about 200k bytes a parse's working set still fits, so the smaller run is the one that fits and a
// linear parse shows a ratio near the bound. A quadratic parse takes about 16 times longer at 4n; the
// bound, 7.75, is the 30-at-10n this test used to apply, restated for 4n (the same growth exponent,
// about 1.48).
//
// Each size is timed twice, the two sizes interleaved so a burst of load meets both, and the best of
// each kept; a timing spans at least 20ms (timeBoth). A family over the bound is measured again, twice at most, and fails only when every
// measurement is over: a super-linear family is over every time, a burst of load on a shared machine
// is not.
//
// A family that parses the small input in more than budget, in each of three tries, fails before
// any of that: a linear family parses 200k bytes in milliseconds, and a quadratic one takes seconds
// there and many times that at 4n, so it is caught by one parse rather than timed until the test's
// deadline.
func TestLinearTime(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing measurement: not under -short, nor under -race's instrumentation")
	}
	const n, k, bound, budget = 200_000, 4, 7.75, 250 * time.Millisecond
	for _, f := range linearFamilies() {
		t.Run(f.name, func(t *testing.T) {
			small, large := []byte(f.gen(n)), []byte(f.gen(k*n))
			if d := onceBest(small, f.opts, budget); d > budget {
				t.Errorf("%d bytes took %v in each of 3 parses, past the %v a linear parse needs — not linear", len(small), d, budget)
				return
			}
			var ratios []float64
			for attempt := 0; attempt < 3; attempt++ {
				ts, tl := timeBoth(small, large, f.opts)
				ratios = append(ratios, float64(tl)/float64(max(ts, time.Microsecond)))
				if ratios[attempt] <= bound {
					break
				}
			}
			t.Logf("%7d bytes, %8d bytes, ratios %.1f", len(small), len(large), ratios)
			if len(ratios) == 3 && slices.Min(ratios) > bound {
				t.Errorf("%dx the input took %.1fx the time in each of %d measurements — not linear", k, ratios, len(ratios))
			}
		})
	}
}

// onceBest is the quickest of up to three single parses of src, stopping at the first within budget.
func onceBest(src []byte, opts []markdown.Option, budget time.Duration) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 3 {
		runtime.GC()
		start := time.Now()
		markdown.Parse(src, opts...)
		if best = min(best, time.Since(start)); best <= budget {
			break
		}
	}
	return best
}

// timeBoth is the best of two timings of small and of large, interleaved, the garbage of each
// collected outside the timing. A timing parses its input again until 20ms have passed and is the
// time per parse: a family that parses 200k bytes in under a millisecond would otherwise be timed
// over a few scheduler slices, and one slice lost to another process would double it.
func timeBoth(small, large []byte, opts []markdown.Option) (ts, tl time.Duration) {
	const window = 20 * time.Millisecond
	perParse := func(src []byte) time.Duration {
		runtime.GC()
		start, n := time.Now(), 0
		for n == 0 || time.Since(start) < window {
			markdown.Parse(src, opts...)
			n++
		}
		return time.Since(start) / time.Duration(n)
	}
	ts, tl = time.Duration(1<<63-1), time.Duration(1<<63-1)
	for range 2 {
		ts = min(ts, perParse(small))
		tl = min(tl, perParse(large))
	}
	return ts, tl
}

// BenchmarkParseFamilies parses each family at 200k bytes and reports its cost per input byte:
// ns/byte, and B/byte, what a parse allocates for each byte it reads. Compare runs with benchstat.
func BenchmarkParseFamilies(b *testing.B) {
	for _, f := range linearFamilies() {
		src := []byte(f.gen(200_000))
		b.Run(f.name, func(b *testing.B) {
			b.ReportAllocs()
			var ms0, ms1 runtime.MemStats
			runtime.ReadMemStats(&ms0)
			start := time.Now()
			for b.Loop() {
				markdown.Parse(src, f.opts...)
			}
			elapsed := time.Since(start)
			runtime.ReadMemStats(&ms1)
			per := float64(b.N) * float64(len(src))
			b.ReportMetric(float64(elapsed.Nanoseconds())/per, "ns/byte")
			b.ReportMetric(float64(ms1.TotalAlloc-ms0.TotalAlloc)/per, "B/byte")
		})
	}
}

func TestKindNames(t *testing.T) {
	for k, want := range map[markdown.Kind]string{
		markdown.KindParagraph: "paragraph", markdown.KindHardBreak: "linebreak", markdown.KindTable: "table",
		markdown.KindWikilink: "wikilink", markdown.KindCalloutTitle: "callout_title", markdown.Kind(999): "kind(999)",
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
