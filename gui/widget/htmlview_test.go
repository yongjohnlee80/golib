package widget

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	phtml "github.com/yongjohnlee80/golib/parse/html"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// exportPage is a page as AutoDoc's export writes one: its own stylesheet, with custom properties,
// var(), rem and ch, the font and border shorthands, margin: auto and a collapsed table.
func exportPage(body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><style>` +
		`:root{color-scheme:dark;--background:#101010;--foreground:#e0e0e0;--accent:#5c9cf5;--muted:#888888;--surface:#202020;--border:#444444}` +
		`body{max-width:76ch;margin:3rem auto;padding:0 1.5rem;background:var(--background);color:var(--foreground);font:1rem/1.6 system-ui,sans-serif}` +
		`a{color:var(--accent)}pre,code{background:var(--surface)}pre{padding:1rem;overflow:auto;border:1px solid var(--border)}` +
		`blockquote{border-left:.2rem solid var(--accent);padding-left:1rem;color:var(--muted)}table{border-collapse:collapse}th,td{border:1px solid var(--border);padding:.3rem .6rem}` +
		`pre.mermaid{background:none;border:0;text-align:center;color:var(--foreground)}</style></head><body>` + body + `</body></html>`
}

// pixelView is an HTMLView bound to its pixel layout and painted once on a recording canvas of
// width × height.
func pixelView(t *testing.T, html string, width, height float32) (*tuiwidget.HTMLView, *htmlLayout, *gui.RecordingCanvas) {
	t.Helper()
	v := tuiwidget.NewHTMLView()
	v.SetHTML([]byte(html))
	BindHTML(v)
	l := v.BoundLayout().(*htmlLayout)
	c := gui.NewRecordingCanvas(gui.Size{W: width, H: height}, gui.Size{W: 8, H: 19.2})
	c.TextPx = 16
	c.Colors = func(int, int) (color.NRGBA, color.NRGBA) {
		return color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
	}
	l.Paint(c)
	return v, l, c
}

// boxOf is the first laid-out box made from an element named name.
func (l *htmlLayout) boxOf(name string) *box {
	var found *box
	var walk func(bx *box)
	walk = func(bx *box) {
		if found != nil {
			return
		}
		if bx.node != nil && bx.node.Name == name {
			found = bx
			return
		}
		for _, k := range bx.kids {
			walk(k)
		}
	}
	for _, b := range l.blocks {
		if b.laid {
			walk(b.box)
		}
	}
	return found
}

// TestHTMLLayoutReadsTheExport: AutoDoc's own export page lays out as its stylesheet says: the
// body 76ch wide, centred; a heading at twice the text; links in --accent; pre on --surface with
// its border; a table's cells bordered and collapsed; a quote barred in the accent.
func TestHTMLLayoutReadsTheExport(t *testing.T) {
	_, l, c := pixelView(t, exportPage(`<h1 data-src="0-8">Title</h1><p data-src="9-40">see <a href="b.md">the link</a> and <code>code</code></p>`+
		`<pre data-src="41-60"><code>func x() {}</code></pre><blockquote data-src="61-70"><p>quoted</p></blockquote>`+
		`<table data-src="71-90"><tr><th>H</th><th>I</th></tr><tr><td>a</td><td>b</td></tr></table>`), 1200, 800)
	content := l.contentWidth()
	if want := float32(76 * 16 * 0.55); content < want-1 || content > want+1 {
		t.Errorf("the body is %v wide, want 76ch (%v)", content, want)
	}
	// centred: margin auto around the content and its 1.5rem padding each side
	if left, want := l.bodyLeft(), (1200-(content+48))/2+24; left < want-1 || left > want+1 {
		t.Errorf("the body's content starts at %v, want %v (centred in 1200)", left, want)
	}
	h1 := l.boxOf("h1")
	if h1 == nil || h1.st.fontSize != 32 || !h1.st.bold {
		t.Fatalf("h1 style %+v, want 32px bold", h1)
	}
	if a := l.runs[1].spans; len(a) < 2 || a[1].Link != "b.md" || a[1].Color != (color.NRGBA{0x5c, 0x9c, 0xf5, 0xff}) {
		t.Errorf("the link span is %+v, want b.md in --accent", a)
	}
	pre := l.boxOf("pre")
	if pre == nil || pre.st.background != (color.NRGBA{0x20, 0x20, 0x20, 0xff}) || !pre.st.overflowScroll {
		t.Errorf("pre style %+v, want --surface and overflow: auto", pre.st)
	}
	if pre.st.border[0].color != (color.NRGBA{0x44, 0x44, 0x44, 0xff}) || pre.st.border[0].w.px(16, 16, 0) != 1 {
		t.Errorf("pre border %+v, want 1px --border", pre.st.border[0])
	}
	bq := l.boxOf("blockquote")
	if bq == nil || bq.st.border[3].color != (color.NRGBA{0x5c, 0x9c, 0xf5, 0xff}) || bq.st.border[3].w.px(16, 16, 0) != 3.2 {
		t.Errorf("blockquote's bar %+v, want .2rem --accent", bq.st.border[3])
	}
	tbl := l.boxOf("table")
	if tbl == nil || !tbl.st.collapse || len(tbl.kids) != 2 || len(tbl.kids[0].kids) != 2 {
		t.Fatalf("table %+v, want two rows of two cells, collapsed", tbl)
	}
	if a, b := tbl.kids[0].kids[0], tbl.kids[1].kids[0]; a.w != b.w {
		t.Errorf("a column's cells differ in width: %v and %v", a.w, b.w)
	}
	var drewBody bool
	for _, call := range c.Calls {
		if call.Op == "FillRect" && call.Brush.Color == (color.NRGBA{0x10, 0x10, 0x10, 0xff}) {
			drewBody = true
		}
	}
	if !drewBody {
		t.Error("the body's --background was not painted")
	}
}

// TestHTMLLayoutSelectsAndCopies: positions run through a paragraph, a list item and a table's
// cells; the copied text has a line between blocks and a tab between cells.
func TestHTMLLayoutSelectsAndCopies(t *testing.T) {
	_, l, _ := pixelView(t, `<p>alpha beta</p><ul><li>gamma</li></ul><table><tr><td>c1</td><td>c2</td></tr></table>`, 600, 400)
	if len(l.runs) != 4 {
		t.Fatalf("%d runs, want 4 (a paragraph, an item, two cells)", len(l.runs))
	}
	from := tuiwidget.DocPos{Block: 0, Span: 0, Offset: 6}
	to := l.endOf(3)
	if got := l.Text(from, to); got != "beta\ngamma\nc1\tc2" {
		t.Errorf("Text = %q", got)
	}
	if got := l.Text(to, from); got != "beta\ngamma\nc1\tc2" {
		t.Errorf("Text backwards = %q", got)
	}
	if rs := l.Rects(from, to); len(rs) != 4 {
		t.Errorf("Rects = %v, want one per run", rs)
	}
	// At inverts a point in the first run
	r := l.runs[0]
	caret := r.para.Caret(flowPos(from))
	if p := l.At(r.ax+caret.X+0.1, r.ay+caret.Y+caret.H/2); p != from {
		t.Errorf("At the caret of %v = %v", from, p)
	}
}

// TestHTMLLayoutLinksAndSourceSpans: LinkAt answers the link under a point only; BlockAt and
// BlockTop speak in data-src, the block after a byte answering for a byte between blocks.
func TestHTMLLayoutLinksAndSourceSpans(t *testing.T) {
	_, l, _ := pixelView(t, `<p data-src="0-10">go <a href="n.md">there</a></p><p>no source</p><p data-src="50-60">last</p>`, 600, 400)
	r := l.runs[0]
	var linkFrag gui.Rect
	for _, ln := range r.para.Lines {
		for _, f := range ln.Frags {
			if r.spans[f.Span].Link != "" {
				linkFrag = gui.Rect{X: r.ax + f.X, Y: r.ay + ln.Y, W: f.W, H: ln.H}
			}
		}
	}
	if got := l.LinkAt(linkFrag.X+1, linkFrag.Y+1); got != "n.md" {
		t.Errorf("LinkAt on the link = %q", got)
	}
	if got := l.LinkAt(r.ax+1, linkFrag.Y+1); got != "" {
		t.Errorf("LinkAt off the link = %q", got)
	}
	top, ok := l.BlockTop(20)
	if !ok || top != l.blocks[2].y {
		t.Errorf("BlockTop(20) = %v %v, want the last block's top %v", top, ok, l.blocks[2].y)
	}
	if src, y := l.BlockAt(l.blocks[1].y + 1); src != -1 || y != l.blocks[1].y {
		t.Errorf("BlockAt a block with no source = %d %v", src, y)
	}
}

// TestHTMLLayoutRelaysOutOnlyWhatChanged: an edit to one paragraph of a long page lays that one
// block out again; the block at the top keeps its place.
func TestHTMLLayoutRelaysOutOnlyWhatChanged(t *testing.T) {
	page := func(edited string) string {
		var sb strings.Builder
		for i := range 2000 {
			text := fmt.Sprintf("paragraph %d of a long note", i)
			if i == 3 {
				text = edited
			}
			fmt.Fprintf(&sb, `<p data-src="%d-%d">%s</p>`, i*100, i*100+99, text)
		}
		return sb.String()
	}
	v, l, _ := pixelView(t, page("the third"), 600, 400)
	if l.relaid >= 2000 {
		t.Fatalf("the first paint laid out %d blocks: not lazily", l.relaid)
	}
	before := l.relaid
	topBefore := l.blocks[5].y
	v.SetHTML([]byte(page("the third, edited")))
	c := gui.NewRecordingCanvas(gui.Size{W: 600, H: 400}, gui.Size{W: 8, H: 19.2})
	c.TextPx = 16
	c.Colors = func(int, int) (color.NRGBA, color.NRGBA) {
		return color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
	}
	l.Paint(c)
	if got := l.relaid - before; got != 1 {
		t.Errorf("an edit to one paragraph laid out %d blocks, want 1", got)
	}
	if l.blocks[5].y != topBefore {
		t.Errorf("a block below the edit moved from %v to %v (the edit did not change its height)", topBefore, l.blocks[5].y)
	}
}

// TestHTMLLayoutRobust: what a page should not do never breaks the layout.
func TestHTMLLayoutRobust(t *testing.T) {
	for name, src := range map[string]string{
		"unknown":  `<foo><bar baz=1>text</bar></foo><blink>x</blink>`,
		"css":      `<style>p{color:;font:;margin:1px 2px 3px 4px 5px;display:flex}@media(x){p{color:red}}a::before{x:y}}}</style><p style="color: rgb(1,2">x</p>`,
		"script":   `<p>a</p><script>if (a < b) { document.write("<p>x</p>") }</script><p>b</p>`,
		"broken":   `<p><b>bold <i>both</b> italic</i><td>cell</tr></table><li>`,
		"deep":     strings.Repeat("<div>", 10000) + "deep" + strings.Repeat("</div>", 10000),
		"table":    `<table><tr></tr><tr><td></td></tr><td>loose</td></table><table></table>`,
		"pre":      `<pre>` + strings.Repeat("x", 5000) + `</pre>`,
		"images":   `<img src="x.png" alt="alt"><img><img src="data:image/png;base64,!!!">`,
		"empty":    ``,
		"lists":    `<ol start="x"><li>a<ol><li>b</li></ol></li></ol><li>loose</li>`,
		"var loop": `<style>:root{--a:var(--b);--b:var(--a)}p{color:var(--a)}</style><p>x</p>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, l, _ := pixelView(t, src, 500, 300)
			_ = l.Height()
			_ = l.At(10, 10)
			_ = l.Text(tuiwidget.DocPos{}, tuiwidget.DocPos{Block: 99})
		})
	}
}

// TestHTMLCSSCascade: specificity orders rules, source order breaks ties, !important wins over
// specificity, a style attribute over any rule, and var() falls back when a property is unset.
func TestHTMLCSSCascade(t *testing.T) {
	// the more specific rules come first, so source order alone would pick the wrong ones
	_, l, _ := pixelView(t, `<style>#i{color:#444444}p.a{color:#333333}.a{color:#222222}p{color:#111111}`+
		`p.b{color:#555555 !important}#j{color:#666666}.c{color:var(--none, #777777)}`+
		`div p{margin-left:5px} div > p{margin-left:7px}</style>`+
		`<p class="a">1</p><p class="a" id="i">2</p><p class="b" id="j">3</p><p class="a" style="color:#888888">4</p><p class="c">5</p>`+
		`<div><p>6</p></div><div><section><p>7</p></section></div>`, 600, 400)
	want := []uint8{0x33, 0x44, 0x55, 0x88, 0x77}
	for i, w := range want {
		if got := l.runs[i].spans[0].Color; got.R != w {
			t.Errorf("paragraph %d colour %#x, want %#x", i+1, got.R, w)
		}
	}
	six, seven := l.runs[5], l.runs[6]
	if six.ax-l.bodyLeft() != 7 || seven.ax-l.bodyLeft() != 5 {
		t.Errorf("child over descendant: margins %v and %v, want 7 and 5", six.ax-l.bodyLeft(), seven.ax-l.bodyLeft())
	}
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestHTMLImagesAreContained: a data: URI decodes; one over its cap, an image past the size caps
// and any non-data source with no resolver do not; SVG is never drawn.
func TestHTMLImagesAreContained(t *testing.T) {
	ctx := context.Background()
	small := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngOf(t, 4, 3))
	got, err := loadImage(ctx, small, nil)
	if img, ok := got.(image.Image); err != nil || !ok || img.Bounds().Dx() != 4 {
		t.Fatalf("a small data: PNG: %v %v", got, err)
	}
	big := "data:image/png;base64," + strings.Repeat("A", (9<<20)*4/3)
	if _, err := loadImage(ctx, big, nil); !errors.Is(err, errImageCap) {
		t.Errorf("a 9 MiB data: URI: %v, want the cap", err)
	}
	// a header claiming 10000 × 10000 is refused before decoding
	huge := pngOf(t, 1, 1)
	copy(huge[16:24], []byte{0, 0, 0x27, 0x10, 0, 0, 0x27, 0x10})
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29])) // the header's checksum, kept valid
	if _, err := loadImage(ctx, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(huge), nil); !errors.Is(err, errImageCap) {
		t.Errorf("a 10000 × 10000 PNG: %v, want the cap", err)
	}
	for _, s := range []string{"x.svg", "data:image/svg+xml,<svg/>", "img/a.SVG?v=1"} {
		if !isSVG(s) {
			t.Errorf("%q is not seen as SVG", s)
		}
	}
	// unmounted, nothing loads: an image shows its alt text
	_, l, _ := pixelView(t, `<p><img src="img/a.png" alt="a picture"></p>`, 400, 200)
	if sp := l.runs[0].spans; len(sp) != 1 || sp[0].Text != "a picture" || !sp[0].Font.Italic {
		t.Errorf("an image that cannot load shows %+v, want its alt text", sp)
	}
}

// TestHTMLLayoutScrollsAPreSideways: a pre wider than the page scrolls inside its box, and its
// runs' positions follow.
func TestHTMLLayoutScrollsAPreSideways(t *testing.T) {
	_, l, _ := pixelView(t, `<pre style="overflow:auto">`+strings.Repeat("wide ", 100)+`</pre>`, 300, 200)
	r := l.runs[0]
	x0 := r.ax
	pre := l.boxOf("pre")
	if !l.ScrollX(pre.x+5, l.blocks[0].y+pre.y+5, 40) {
		t.Fatal("ScrollX over the pre did nothing")
	}
	if r.ax != x0-40 {
		t.Errorf("the run's origin %v after scrolling 40, want %v", r.ax, x0-40)
	}
	if l.ScrollX(pre.x+5, l.blocks[0].y+pre.y+5, -1000); r.ax != x0 {
		t.Errorf("scrolling back past the start left the run at %v, want %v", r.ax, x0)
	}
}

func flowPos(p tuiwidget.DocPos) (fp struct{ Span, Offset int }) {
	fp.Span, fp.Offset = p.Span, p.Offset
	return fp
}

var _ = phtml.Parse

// repaint paints l again on a canvas like pixelView's.
func repaint(l *htmlLayout, width, height float32) {
	c := gui.NewRecordingCanvas(gui.Size{W: width, H: height}, gui.Size{W: 8, H: 19.2})
	c.TextPx = 16
	c.Colors = func(int, int) (color.NRGBA, color.NRGBA) {
		return color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
	}
	l.Paint(c)
}

// TestHTMLLayoutIdenticalBlocksStayApart: two identical paragraphs share a key, yet after a
// rebuild each keeps its own layout: their runs sit at their own places, and a point in the second
// is in the second.
func TestHTMLLayoutIdenticalBlocksStayApart(t *testing.T) {
	page := `<p>same</p><p>same</p><p>last</p>`
	v, l, _ := pixelView(t, page, 600, 400)
	v.SetHTML([]byte(page))
	repaint(l, 600, 400)
	if a, b := l.runs[0].ay, l.runs[1].ay; a == b {
		t.Fatalf("the two paragraphs' runs are both at y %v", a)
	}
	r := l.runs[1]
	if p := l.At(r.ax+1, r.ay+1); p.Block != 1 {
		t.Errorf("a point in the second paragraph is in run %d", p.Block)
	}
}

// TestHTMLLayoutSourceSpansInsideLists: an item's own source bytes answer BlockAt and BlockTop,
// though the list around it has none.
func TestHTMLLayoutSourceSpansInsideLists(t *testing.T) {
	_, l, _ := pixelView(t, `<ul><li data-src="0-5">a</li><li data-src="6-11">b</li></ul><p data-src="12-20">after</p>`, 600, 400)
	second := l.runs[1]
	top, ok := l.BlockTop(7)
	if !ok || top > second.ay || top < second.ay-20 {
		t.Errorf("BlockTop(7) = %v %v, want the second item's top (its run at %v)", top, ok, second.ay)
	}
	if src, _ := l.BlockAt(second.ay + 1); src != 6 {
		t.Errorf("BlockAt in the second item = %d, want 6", src)
	}
}

// TestHTMLLayoutRemDoesNotCompound: html{font-size:2rem} is twice the window's text however often
// the page is built again.
func TestHTMLLayoutRemDoesNotCompound(t *testing.T) {
	page := `<html><head><style>html{font-size:2rem}</style></head><body><p>x</p></body></html>`
	v, l, _ := pixelView(t, page, 600, 400)
	for range 3 {
		v.SetHTML([]byte(page))
		repaint(l, 600, 400)
	}
	if l.rootPx != 32 || l.runs[0].spans[0].Font.Size != 32 {
		t.Errorf("after four builds the root is %vpx and the text %vpx, want 32", l.rootPx, l.runs[0].spans[0].Font.Size)
	}
}

// TestHTMLLayoutBytesAfterTheLastBlock: a source byte after every block (the blank lines that end
// a note, where an editor's cursor can be) is the last block's.
func TestHTMLLayoutBytesAfterTheLastBlock(t *testing.T) {
	_, l, _ := pixelView(t, `<p data-src="0-10">a</p><p data-src="12-20">b</p>`, 600, 400)
	if top, ok := l.BlockTop(25); !ok || top != l.blocks[1].y {
		t.Errorf("BlockTop(25) = %v %v, want the last block's top %v", top, ok, l.blocks[1].y)
	}
}
