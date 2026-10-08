package widget

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
)

// TestCSSMaths: calc(), min(), max() and clamp() with every unit evaluate where their length is:
// archive.css's own values at two view widths; a malformed or over-bound expression is refused.
func TestCSSMaths(t *testing.T) {
	wide, narrow := gui.Size{W: 1440, H: 900}, gui.Size{W: 800, H: 600}
	for _, c := range []struct {
		v    string
		of   float32
		view gui.Size
		want float32
	}{
		{"calc(100% - 2.5rem)", 1000, wide, 960},
		{"min(100% - 2.5rem, 940px)", 1000, wide, 940},
		{"min(100% - 2.5rem, 940px)", 800, narrow, 760},
		{"max(1.25rem, calc((100vw - 1040px)/2))", 0, wide, 200},
		{"max(1.25rem, calc((100vw - 1040px)/2))", 0, narrow, 20},
		{"clamp(2rem, 5vw, 3.3rem)", 0, wide, 52.8},
		{"clamp(2rem, 5vw, 3.3rem)", 0, narrow, 40},
		{"clamp(2rem, 5vw, 3.3rem)", 0, gui.Size{W: 400}, 32},
		{"calc(2 * 3px + 1px)", 0, wide, 7},
		{"calc(-1px + 4px)", 0, wide, 3},
		{"calc(10px / 4)", 0, wide, 2.5},
		{"10vw", 0, gui.Size{W: 500}, 50},
		{"50vh", 0, gui.Size{H: 600}, 300},
		{"calc(2em + 1ch)", 0, wide, 32 + 16*0.55},
	} {
		l, ok := parseLen(c.v)
		if !ok {
			t.Errorf("%s: not read", c.v)
			continue
		}
		if got := l.px(16, 16, c.of, c.view); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("%s at %v (of %v) = %v, want %v", c.v, c.view, c.of, got, c.want)
		}
	}
	for _, bad := range []string{"calc(1px +)", "calc(1px", "min()", "clamp(1px, 2px)", "foo(1px)", "calc(1px + auto)",
		"calc(" + strings.Repeat("1px + ", 100) + "1px)"} {
		if _, ok := parseLen(bad); ok {
			t.Errorf("%q was read", bad)
		}
	}
}

// TestCSSSelectors: attribute selectors of each operator, :first-child, :last-child and :not()
// match; :hover and the like parse and never apply; a sibling combinator, a pseudo-element and an
// unknown pseudo-class drop their selector; attributes and pseudo-classes weigh as classes.
func TestCSSSelectors(t *testing.T) {
	red := color.NRGBA{R: 0xff, A: 0xff}
	page := `<html data-collection="Catalog"><head><style>
p{color:#000000}
p.w:not(.z){color:#000001}
p.w{color:#ff0000}
html[data-collection="Catalog"] .a{color:#ff0000}
p[title]{color:#ff0000}
p[lang|=en]{color:#ff0000}
p[class~="x"]{color:#ff0000}
p[data-k^="pre"]{color:#ff0000}
p[data-k$="end"]{color:#ff0000}
p[data-k*="mid"]{color:#ff0000}
p[data-q="A" i]{color:#ff0000}
div > p:first-child{color:#ff0000}
section p:last-child{color:#ff0000}
p.n:not(.skip){color:#ff0000}
p.h:hover{color:#ff0000}
p.s + p{color:#ff0000}
p.e::before{color:#ff0000}
p.u:nth-child(2){color:#ff0000}
</style></head><body>
<p class="a">a</p><p title="t">title</p><p lang="en-GB">lang</p><p class="w x">word</p>
<p data-k="prefix">pre</p><p data-k="the end">end</p><p data-k="a mid b">mid</p><p data-q="a">fold</p>
<div><p>first</p><p>second</p></div><section><p>one</p><p>last</p></section>
<p class="n">not</p><p class="n skip">skipped</p><p class="h">hover</p><p class="s">s</p><p>sibling</p>
<p class="e">pseudo</p><p class="u">nth</p>
</body></html>`
	_, l, _ := pixelView(t, page, 600, 2000)
	l.layOutTo(1e9)
	got := map[string]color.NRGBA{}
	for _, r := range l.runs {
		if len(r.spans) > 0 {
			got[r.spans[0].Text] = r.spans[0].Color
		}
	}
	for _, text := range []string{"a", "title", "lang", "pre", "end", "mid", "fold", "first", "last", "not"} {
		if got[text] != red {
			t.Errorf("%q is %v, want red", text, got[text])
		}
	}
	for _, text := range []string{"second", "one", "skipped", "hover", "sibling", "pseudo", "nth"} {
		if got[text] == red {
			t.Errorf("%q is red", text)
		}
	}
	if got["word"] != (color.NRGBA{B: 1, A: 0xff}) {
		t.Errorf("p.w:not(.z) (specificity 21), first in the sheet, lost to the later p.w or p[class~=x] (11 each): %v", got["word"])
	}
}

// TestCSSMedia: a query's type and features against the view's size and theme; a rule in a
// matching block applies and one in another does not, at two widths.
func TestCSSMedia(t *testing.T) {
	env := mediaEnv{w: 500, h: 800}
	for q, want := range map[string]bool{
		"(max-width: 600px)": true, "(max-width: 400px)": false, "(min-width: 30em)": true,
		"screen and (min-width: 30em)": true, "print": false, "not print": true, "all, print": true,
		"(orientation: portrait)": true, "(prefers-color-scheme: dark)": false, "(prefers-color-scheme: light)": true,
		"(prefers-reduced-motion: reduce)": false, "(hover: hover)": false, "only screen and (max-width: 499px)": false,
	} {
		if got := mediaMatches(q, env); got != want {
			t.Errorf("%q at 500×800: %v, want %v", q, got, want)
		}
	}
	if !mediaMatches("(prefers-color-scheme: dark)", mediaEnv{dark: true}) {
		t.Error("prefers-color-scheme: dark in a dark theme")
	}
	page := `<html><head><style>p{color:#000000} @media (max-width: 600px) { p{color:#ff0000} } @media print { p{color:#00ff00} }</style></head><body><p>x</p></body></html>`
	_, narrow, _ := pixelView(t, page, 400, 300)
	_, wide, _ := pixelView(t, page, 900, 300)
	if c := narrow.runs[0].spans[0].Color; c != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Errorf("at 400 px: %v, want the max-width rule's red", c)
	}
	if c := wide.runs[0].spans[0].Color; c != (color.NRGBA{A: 0xff}) {
		t.Errorf("at 900 px: %v, want black", c)
	}
}

// TestCSSText: text-transform changes what is shown, letter-spacing (in em, against the span's own
// size) reaches the span, a numeric weight from 600 is bold, and font-family reaches the font as a
// list the shaper reads (system-ui as sans-serif), through the font shorthand too; all inherit.
func TestCSSText(t *testing.T) {
	page := `<html><head><style>
.eyebrow{font-size:10px;font-weight:750;text-transform:uppercase;letter-spacing:.12em}
article{font-family:Georgia, serif} body{font:17px/1.75 system-ui, sans-serif}
.cap{text-transform:capitalize} .light{font-weight:650} .thin{font-weight:500}
</style></head><body><div class="eyebrow">Aesop / reading <b>edition</b></div>
<article><p>serif text</p></article><p class="cap">the hares-and foxes</p><p class="light">w650</p><p class="thin">w500</p><p>body</p></body></html>`
	_, l, _ := pixelView(t, page, 600, 2000)
	l.layOutTo(1e9)
	sp := func(i int) flowSpan { return flowSpan(l.runs[i].spans[0]) }
	if s := sp(0); s.Text != "AESOP / READING " || !s.Font.Bold || s.LetterSpacing < 1.19 || s.LetterSpacing > 1.21 {
		t.Errorf("the eyebrow: %q bold %v spacing %v, want upper-cased, bold, 1.2 px", s.Text, s.Font.Bold, s.LetterSpacing)
	}
	if s := l.runs[0].spans[1]; s.Text != "EDITION" || s.LetterSpacing < 1.19 {
		t.Errorf("an inline child: %q spacing %v, want both inherited", s.Text, s.LetterSpacing)
	}
	if f := sp(1).Font.Family; f != "Georgia, serif" {
		t.Errorf("the article's family %q", f)
	}
	if s := sp(2); s.Text != "The Hares-And Foxes" {
		t.Errorf("capitalized: %q", s.Text)
	}
	if !sp(3).Font.Bold || sp(4).Font.Bold {
		t.Errorf("weights 650 and 500: bold %v and %v, want true and false", sp(3).Font.Bold, sp(4).Font.Bold)
	}
	if f := sp(5).Font.Family; f != "sans-serif, sans-serif" {
		t.Errorf("the body's family from the font shorthand: %q", f)
	}
}

type flowSpan = flow.Span
