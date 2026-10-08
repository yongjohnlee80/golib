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

// TestCSSBox: box-sizing, width bounds and heights size a box as CSS does.
func TestCSSBox(t *testing.T) {
	page := `<html><head><style>body{margin:0}
.bb{box-sizing:border-box;width:200px;padding:10px;border:5px solid}
.cb{width:200px;padding:10px;border:5px solid}
.h{height:50px} .minh{min-height:80px} .maxh{max-height:20px}
.minw{width:10px;min-width:120px} .clip{max-width:100px}
</style></head><body><div class="bb">a</div><div class="cb">b</div><div class="h">c</div>
<div class="minh">d</div><div class="maxh">e<br>f<br>g</div><div class="minw">h</div><div class="clip">i</div></body></html>`
	_, l, _ := pixelView(t, page, 600, 1000)
	l.layOutTo(1e9)
	box := func(i int) *box { return l.blocks[i].box }
	for _, c := range []struct {
		what string
		i    int
		w, h float32
	}{{"border-box 200", 0, 200, -1}, {"content-box 200 + 30", 1, 230, -1}, {"height 50", 2, -1, 50},
		{"min-height 80", 3, -1, 80}, {"max-height 20", 4, -1, 20}, {"min-width over width", 5, 120, -1}, {"max-width", 6, 100, -1}} {
		b := box(c.i)
		if c.w >= 0 && (b.w < c.w-0.01 || b.w > c.w+0.01) {
			t.Errorf("%s: width %v, want %v", c.what, b.w, c.w)
		}
		if c.h >= 0 && (b.h < c.h-0.01 || b.h > c.h+0.01) {
			t.Errorf("%s: height %v, want %v", c.what, b.h, c.h)
		}
	}
}

// TestCSSPositioning: an absolutely positioned box takes no place in the flow and sits at its
// insets; one under the body wholly above the page is never drawn (the skip link); a relative one
// moves and keeps its place; visibility: hidden keeps its place and draws nothing.
func TestCSSPositioning(t *testing.T) {
	page := `<html><head><style>body{margin:0} p{margin:0}
.skip{position:absolute;top:-100px} .card{position:relative} .badge{position:absolute;top:5px;left:7px}
.nudge{position:relative;top:10px;left:4px} .ghost{visibility:hidden}
</style></head><body><a class="skip" href="#d">Skip to document</a><p>first</p>
<div class="card"><p>in the card</p><span class="badge">badge</span><p>after the badge</p></div>
<p class="nudge">nudged</p><p class="ghost">ghost</p><p>last</p></body></html>`
	_, l, rc := pixelView(t, page, 600, 400)
	l.layOutTo(1e9)
	for _, c := range rc.Calls {
		if c.Op == "DrawText" && c.Text != nil && c.Rect.Y < 0 {
			t.Errorf("drew text above the page at %v", c.Rect)
		}
	}
	if len(l.abs) != 1 || l.abs[0].box == nil || l.abs[0].box.y > -99 {
		t.Fatalf("the skip link: %d absolute blocks, %+v", len(l.abs), l.abs)
	}
	if first := l.blocks[0].box; first == nil || l.blocks[0].y > 0.01 {
		t.Errorf("the first block is at %v: the skip link took a place in the flow", l.blocks[0].y)
	}
	var card *box
	for _, b := range l.blocks {
		if b.node != nil && b.node.Name == "div" {
			card = b.box
		}
	}
	if card == nil || len(card.kids) != 3 {
		t.Fatalf("the card: %+v", card)
	}
	inCard, badge, after := card.kids[0], card.kids[1], card.kids[2]
	if after.y < inCard.y+inCard.h-0.01 || after.y > inCard.y+inCard.h+0.01 {
		t.Errorf("the paragraph after the badge is at %v, want right after the first (%v): the badge took a place", after.y, inCard.y+inCard.h)
	}
	if badge.y < card.y+4.99 || badge.y > card.y+5.01 || badge.x < card.x+6.99 || badge.x > card.x+7.01 {
		t.Errorf("the badge at (%v,%v), want (%v,%v)", badge.x, badge.y, card.x+7, card.y+5)
	}
	var nudged, ghost, last *topBlock
	for _, b := range l.blocks {
		if b.node == nil || b.node.Name != "p" {
			continue
		}
		switch {
		case strings.Contains(b.node.Children[0].Data, "nudged"):
			nudged = b
		case strings.Contains(b.node.Children[0].Data, "ghost"):
			ghost = b
		case strings.Contains(b.node.Children[0].Data, "last"):
			last = b
		}
	}
	if nudged.box.y != 10 || nudged.box.x != 4 {
		t.Errorf("the relative box at (%v,%v) in its block, want (4,10)", nudged.box.x, nudged.box.y)
	}
	if ghost.h <= 0 || last.y < ghost.y+ghost.h-0.01 {
		t.Errorf("the hidden paragraph took no place: %v tall, last at %v", ghost.h, last.y)
	}
	for _, c := range rc.Calls {
		if c.Op == "DrawText" && c.Text != nil && c.Rect.Y >= ghost.y && c.Rect.Y < ghost.y+ghost.h-1 {
			t.Errorf("the hidden paragraph was drawn at %v", c.Rect)
		}
	}
}

// TestCSSImageBox: an image's box from its attributes and CSS, max-width keeping the ratio, and
// where object-fit draws a raster in it.
func TestCSSImageBox(t *testing.T) {
	_, l, _ := pixelView(t, `<p>x</p>`, 600, 400)
	st := func(css string) *computed {
		c := &computed{fontSize: 16}
		for _, d := range parseDecls(css) {
			c.apply(d, c, 16, gui.Size{W: 600})
		}
		return c
	}
	for _, c := range []struct {
		css            string
		iw, ih, aw, ah float32
		w, h           float32
	}{
		{"", 72, 72, 0, 0, 72, 72},
		{"height:180px;width:180px", 72, 72, 0, 0, 180, 180},
		{"width:144px", 72, 36, 0, 0, 144, 72},
		{"max-width:100%;height:auto", 1200, 600, 0, 0, 600, 300},
		{"max-height:350px", 400, 700, 0, 0, 200, 350},
		{"", 100, 50, 300, 0, 300, 150},
	} {
		w, h := l.imageBox(st(c.css), c.iw, c.ih, c.aw, c.ah)
		if w < c.w-0.01 || w > c.w+0.01 || h < c.h-0.01 || h > c.h+0.01 {
			t.Errorf("%q on %v×%v: %v×%v, want %v×%v", c.css, c.iw, c.ih, w, h, c.w, c.h)
		}
	}
	sz := gui.Size{W: 200, H: 100}
	for fit, want := range map[string]gui.Rect{"": {W: 200, H: 100}, "fill": {W: 200, H: 100},
		"contain": {X: 50, W: 100, H: 100}, "cover": {Y: -50, W: 200, H: 200}, "none": {X: 75, Y: 25, W: 50, H: 50}} {
		if got := fitted(fit, sz, 50, 50); got != want {
			t.Errorf("object-fit %q: %+v, want %+v", fit, got, want)
		}
	}
}

// flexKids lays page and answers the first top-level block's items.
func flexKids(t *testing.T, page string, width float32) []*box {
	t.Helper()
	_, l, _ := pixelView(t, `<html><head><style>body{margin:0} div,span,a,p{margin:0;padding:0}`+page, width, 1000)
	l.layOutTo(1e9)
	return l.blocks[0].box.kids
}

// TestCSSFlex: a row with a gap; justify-content's values; wrapping; flex-grow; align-items; a
// column; an inline element as an item.
func TestCSSFlex(t *testing.T) {
	near := func(a, b float32) bool { return a > b-0.5 && a < b+0.5 }
	items := `<span style="width:100px;display:block">a</span><span style="width:100px;display:block">b</span><span style="width:100px;display:block">c</span>`
	row := flexKids(t, `nav{display:flex;gap:10px}</style></head><body><nav>`+items+`</nav></body></html>`, 600)
	if len(row) != 3 || !near(row[0].x, 0) || !near(row[1].x, 110) || !near(row[2].x, 220) || !near(row[1].y, row[0].y) {
		t.Fatalf("a row with a 10px gap: %v %v %v", row[0].x, row[1].x, row[2].x)
	}
	for how, want := range map[string][3]float32{
		"flex-start": {0, 100, 200}, "center": {150, 250, 350}, "flex-end": {300, 400, 500},
		"space-between": {0, 250, 500}, "space-around": {50, 250, 450}, "space-evenly": {75, 250, 425},
	} {
		k := flexKids(t, `nav{display:flex;justify-content:`+how+`}</style></head><body><nav>`+items+`</nav></body></html>`, 600)
		if !near(k[0].x, want[0]) || !near(k[1].x, want[1]) || !near(k[2].x, want[2]) {
			t.Errorf("justify-content %s: %v %v %v, want %v", how, k[0].x, k[1].x, k[2].x, want)
		}
	}
	wrap := flexKids(t, `nav{display:flex;flex-wrap:wrap;gap:5px 10px}</style></head><body><nav>`+items+`</nav></body></html>`, 230)
	if !near(wrap[1].y, wrap[0].y) || wrap[2].y <= wrap[0].y || !near(wrap[2].x, 0) {
		t.Errorf("wrapping at 230: %v,%v %v,%v %v,%v", wrap[0].x, wrap[0].y, wrap[1].x, wrap[1].y, wrap[2].x, wrap[2].y)
	}
	grow := flexKids(t, `nav{display:flex} .g{flex:1}</style></head><body><nav><span style="width:100px;display:block">a</span><span class="g">grows</span></nav></body></html>`, 600)
	if !near(grow[1].w, 500) {
		t.Errorf("flex: 1 took %v of the 500 left", grow[1].w)
	}
	tall := `<span style="display:block;height:60px;width:50px">t</span><span style="display:block;width:50px">s</span>`
	for how, wantY := range map[string]float32{"center": 30 - 0, "flex-end": 60} {
		k := flexKids(t, `nav{display:flex;align-items:`+how+`}</style></head><body><nav>`+tall+`</nav></body></html>`, 600)
		short := k[1]
		if how == "center" && !near(short.y+short.h/2, 30) || how == "flex-end" && !near(short.y+short.h, wantY) {
			t.Errorf("align-items %s: the short item at %v..%v", how, short.y, short.y+short.h)
		}
	}
	st := flexKids(t, `nav{display:flex}</style></head><body><nav>`+tall+`</nav></body></html>`, 600)
	if !near(st[1].h, 60) {
		t.Errorf("align-items stretch: the short item is %v tall, want the line's 60", st[1].h)
	}
	col := flexKids(t, `nav{display:flex;flex-direction:column;gap:8px}</style></head><body><nav>`+items+`</nav></body></html>`, 600)
	if !near(col[0].x, col[1].x) || !near(col[1].y, col[0].y+col[0].h+8) {
		t.Errorf("a column with an 8px gap: %v,%v %v,%v", col[0].x, col[0].y, col[1].x, col[1].y)
	}
	nav := flexKids(t, `nav{display:flex;gap:20px}</style></head><body><nav><a href="a">Aesop</a><span>The Hares</span></nav></body></html>`, 600)
	if len(nav) != 2 || nav[1].x <= nav[0].x+nav[0].w || nav[1].x > nav[0].x+nav[0].w+20.5 {
		t.Errorf("inline items side by side with the gap: %v+%v then %v", nav[0].x, nav[0].w, nav[1].x)
	}
}

// TestCSSGrid: columns from lengths, fr, minmax() and repeat(), auto-fit folding empty tracks;
// items in order, row by row, with gaps.
func TestCSSGrid(t *testing.T) {
	near := func(a, b float32) bool { return a > b-0.5 && a < b+0.5 }
	_, l, _ := pixelView(t, `<p>x</p>`, 600, 400)
	st := func(cols string) *computed { return &computed{fontSize: 16, gridCols: cols} }
	for _, c := range []struct {
		cols  string
		width float32
		gap   float32
		n     int
		want  []float32
	}{
		{"minmax(100px, 1fr) 3fr", 400, 0, 4, []float32{100, 300}},
		{"minmax(100px, 1fr) 3fr", 200, 0, 4, []float32{100, 100}},
		{"repeat(auto-fit, minmax(220px, 1fr))", 1000, 16, 3, []float32{322.67, 322.67, 322.67}},
		{"repeat(auto-fill, minmax(220px, 1fr))", 1000, 16, 3, []float32{238, 238, 238, 238}},
		{"repeat(3, 1fr)", 300, 0, 9, []float32{100, 100, 100}},
		{"200px 1fr", 500, 0, 2, []float32{200, 300}},
		{"", 500, 0, 2, []float32{500}},
	} {
		got := l.gridColumns(st(c.cols), c.width, c.gap, c.n)
		ok := len(got) == len(c.want)
		for i := range got {
			ok = ok && i < len(c.want) && near(got[i], c.want[i])
		}
		if !ok {
			t.Errorf("%q at %v: %v, want %v", c.cols, c.width, got, c.want)
		}
	}
	k := flexKids(t, `dl{display:grid;grid-template-columns:100px 1fr;gap:4px 10px} dt,dd{margin:0}</style></head><body><dl><dt>a</dt><dd>one</dd><dt>b</dt><dd>two</dd></dl></body></html>`, 500)
	if len(k) != 4 || !near(k[1].x, 110) || !near(k[1].y, k[0].y) || !near(k[2].x, 0) || !near(k[2].y, k[0].y+k[0].h+4) {
		t.Errorf("a dl grid: %v,%v %v,%v %v,%v", k[0].x, k[0].y, k[1].x, k[1].y, k[2].x, k[2].y)
	}
}

// TestDetailsFoldAndOpen: a <details> without open shows its summary alone, marked ▸; a click on
// the summary opens it (▾, its content laid out) and another folds it; a click elsewhere does
// nothing; the toggle survives a rebuild of the same page and is dropped for another; one with
// open starts open; a link inside still answers LinkAt.
func TestDetailsFoldAndOpen(t *testing.T) {
	page := `<html><head><style>body{margin:0}</style></head><body><details class="toc"><summary>On this page</summary><ul><li><a href="#x">The Hares</a></li></ul></details><p>after</p></body></html>`
	v, l, _ := pixelView(t, page, 600, 400)
	text := func() string {
		l.layOutTo(1e9)
		var parts []string
		for _, r := range l.runs {
			for _, s := range r.spans {
				parts = append(parts, s.Text)
			}
		}
		return strings.Join(parts, "|")
	}
	if got := text(); !strings.Contains(got, "▸ |On this page") || strings.Contains(got, "The Hares") {
		t.Fatalf("folded: %q", got)
	}
	sum := l.runs[0]
	if l.Activate(sum.ax+200, sum.ay+2) != true {
		t.Fatal("a click on the summary did nothing")
	}
	repaint(l, 600, 400)
	if got := text(); !strings.Contains(got, "▾ |On this page") || !strings.Contains(got, "The Hares") {
		t.Fatalf("opened: %q", got)
	}
	var link *box
	for _, r := range l.runs {
		if strings.Contains(r.spans[0].Text, "The Hares") {
			link = r
		}
	}
	if href := l.LinkAt(link.ax+2, link.ay+2); href != "#x" {
		t.Errorf("the link inside: %q", href)
	}
	if l.Activate(link.ax+2, link.ay+2) {
		t.Error("a click on the content activated the details")
	}
	after := l.runs[len(l.runs)-1]
	if l.Activate(after.ax+2, after.ay+2) {
		t.Error("a click after the details activated it")
	}
	v.SetHTML([]byte(page)) // the same page, built again: still open
	repaint(l, 600, 400)
	if !strings.Contains(text(), "The Hares") {
		t.Error("a rebuild of the same page folded it")
	}
	v.SetHTML([]byte(strings.Replace(page, "after", "changed", 1)))
	repaint(l, 600, 400)
	if strings.Contains(text(), "The Hares") {
		t.Error("another page kept the toggle")
	}
	_, open, _ := pixelView(t, strings.Replace(page, `<details class="toc">`, `<details open>`, 1), 600, 400)
	open.layOutTo(1e9)
	if open.runs[0].spans[0].Text != "▾ " {
		t.Errorf("<details open> starts %q", open.runs[0].spans[0].Text)
	}
}
