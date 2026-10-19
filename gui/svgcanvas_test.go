package gui

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
)

// wellFormed fails t unless out is one well-formed XML document.
func wellFormed(t *testing.T, out []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(out))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("not well-formed SVG (%v):\n%s", err, out)
		}
	}
}

// texts are out's <text> elements' words, in order.
func texts(out []byte) []string {
	var got []string
	for _, m := range regexp.MustCompile(`<text [^>]*>([^<]*)</text>`).FindAllSubmatch(out, -1) {
		got = append(got, string(m[1]))
	}
	return got
}

// The shapes are SVG's own (a path keeps its commands, a corner's radius clamped to half the
// shorter side), in their brushes; each is drawn in its canvas's state: under the transforms and
// opacities pushed, and no longer once popped.
func TestSVGCanvasWritesShapesInItsState(t *testing.T) {
	c := NewSVGCanvas(Size{W: 100, H: 50}, nil)
	red := Solid(color.NRGBA{R: 0xff, A: 0xff})
	half := Solid(color.NRGBA{B: 0xff, A: 0x80})
	c.FillRect(Rect{X: 1, Y: 2, W: 3, H: 4}, red)
	c.FillRRect(Rect{W: 10, H: 10}, 2, red)
	c.StrokeRRect(Rect{W: 10, H: 4}, 9, 1.5, half)
	c.FillEllipse(Rect{X: 10, Y: 10, W: 20, H: 10}, red)
	p := (&Path{}).MoveTo(Pt(1, 2)).LineTo(Pt(3, 4)).QuadTo(Pt(5, 6), Pt(7, 8)).CubeTo(Pt(9, 10), Pt(11, 12), Pt(13, 14)).Close()
	c.PushTransform(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(2, 3)).Offset(f32.Pt(5, 6)))
	c.FillPath(p, half)
	c.PushOpacity(0.5)
	c.StrokePath(p, 2, red)
	c.Pop()
	c.Pop()
	c.FillRect(Rect{W: 1, H: 1}, Brush{Gradient: &LinearGradient{To: Pt(10, 0), FromColor: color.NRGBA{A: 0xff}, ToColor: color.NRGBA{R: 0xff, A: 0x40}}})
	c.PushOpacity(0.25) // never popped: drawing nothing more, it writes nothing
	out := c.Bytes()
	wellFormed(t, out)
	s := string(out)
	for _, want := range []string{
		`viewBox="0 0 100 50"`,
		`<rect x="1" y="2" width="3" height="4" fill="#ff0000"/>`,
		`<rect x="0" y="0" width="10" height="10" rx="2" ry="2" fill="#ff0000"/>`,
		`<rect x="0" y="0" width="10" height="4" rx="2" ry="2" fill="none" stroke-width="1.5" stroke="#0000ff" stroke-opacity="0.5"/>`,
		`<ellipse cx="20" cy="15" rx="10" ry="5" fill="#ff0000"/>`,
		`<g transform="matrix(2 0 0 3 5 6)"><path d="M 1 2 L 3 4 Q 5 6 7 8 C 9 10 11 12 13 14 Z" fill="#0000ff" fill-opacity="0.5"/></g>`,
		`<g transform="matrix(2 0 0 3 5 6)" opacity="0.5"><path d="M 1 2 L 3 4 Q 5 6 7 8 C 9 10 11 12 13 14 Z" fill="none" stroke-width="2" stroke="#ff0000"/></g>`,
		`gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="10" y2="0">`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	grad := regexp.MustCompile(`<linearGradient id="([^"]+)"`).FindStringSubmatch(s)
	if grad == nil || !strings.Contains(s, `<rect x="0" y="0" width="1" height="1" fill="url(#`+grad[1]+`)"/>`) {
		t.Errorf("the gradient's rect is not drawn in it, popped back to no transform:\n%s", s)
	}
}

// depth is how many groups are open at s[:i].
func depth(s string, i int) int { return strings.Count(s[:i], "<g") - strings.Count(s[:i], "</g>") }

// A sub-canvas draws at its rect's origin, clipped to its rect in the drawing's coordinates and to
// its parent's clip, at its parent's opacity but none of its transforms; its own Pushes are its
// own, so the parent drawing between them is not inside them.
func TestSVGCanvasSubKeepsItsOwnState(t *testing.T) {
	c := NewSVGCanvas(Size{W: 100, H: 100}, nil)
	c.PushTransform(f32.Affine2D{}.Offset(f32.Pt(40, 40))) // not the sub's
	sub := c.Sub(Rect{X: 10, Y: 20, W: 30, H: 40})
	if sub.Size() != (Size{W: 30, H: 40}) {
		t.Errorf("the sub-canvas is %+v", sub.Size())
	}
	black := Solid(color.NRGBA{A: 0xff})
	sub.FillRect(Rect{W: 5, H: 5}, black)
	sub.PushOpacity(0.5)
	c.FillRect(Rect{X: 50, Y: 50, W: 7, H: 7}, black) // the parent, between the sub's Push and Pop
	sub.FillRect(Rect{W: 6, H: 6}, black)
	inner := sub.Sub(Rect{X: 5, Y: 5, W: 100, H: 100})
	inner.FillRect(Rect{W: 8, H: 8}, black)
	sub.Pop()
	out := c.Bytes()
	wellFormed(t, out)
	s := string(out)
	ids := regexp.MustCompile(`<clipPath id="([^"]+)"><rect x="([^"]+)" y="([^"]+)" width="([^"]+)" height="([^"]+)"/></clipPath>`).FindAllStringSubmatch(s, -1)
	if len(ids) != 2 || ids[0][2] != "10" || ids[0][3] != "20" || ids[0][4] != "30" || ids[1][2] != "15" || ids[1][3] != "25" {
		t.Fatalf("the clips are not each sub's rect in the drawing's coordinates: %q\n%s", ids, s)
	}
	c1, c2 := ids[0][1], ids[1][1]
	for _, want := range []string{
		`<g clip-path="url(#` + c1 + `)"><g transform="translate(10 20)"><rect x="0" y="0" width="5" height="5" fill="#000000"/></g></g>`,
		`<g clip-path="url(#` + c1 + `)"><g transform="translate(10 20)" opacity="0.5"><rect x="0" y="0" width="6" height="6" fill="#000000"/></g></g>`,
		`<g clip-path="url(#` + c1 + `)"><g clip-path="url(#` + c2 + `)"><g transform="translate(15 25)" opacity="0.5"><rect x="0" y="0" width="8" height="8" fill="#000000"/></g></g></g>`,
		`<g transform="matrix(1 0 0 1 40 40)"><rect x="50" y="50" width="7" height="7" fill="#000000"/></g>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	parent := strings.Index(s, `<g transform="matrix(1 0 0 1 40 40)"><rect x="50"`)
	if parent < 0 || depth(s, parent) != 0 {
		t.Errorf("the parent's rect is drawn inside the sub's groups (depth %d):\n%s", depth(s, parent), s)
	}
}

// Two drawings inlined in one page never share an id.
func TestSVGCanvasIDsAreTheDrawingsOwn(t *testing.T) {
	grad := Brush{Gradient: &LinearGradient{To: Pt(1, 0), FromColor: color.NRGBA{A: 0xff}, ToColor: color.NRGBA{A: 0xff}}}
	var got []string
	for range 2 {
		c := NewSVGCanvas(Size{W: 10, H: 10}, nil)
		c.Sub(Rect{W: 5, H: 5}).FillRect(Rect{W: 1, H: 1}, grad)
		for _, m := range regexp.MustCompile(` id="([^"]+)"`).FindAllStringSubmatch(string(c.Bytes()), -1) {
			if slices.Contains(got, m[1]) {
				t.Errorf("id %q is in both drawings", m[1])
			}
			got = append(got, m[1])
		}
	}
	if len(got) != 4 {
		t.Errorf("ids %q, want a clip and a gradient each", got)
	}
}

// A layout's words are written as <text>, on its baseline, in its font: a line as shaped, a
// wrapped one a <text> per line saying the source's words, a row of runs each at its own x.
func TestSVGCanvasWritesTheWordsALayoutSays(t *testing.T) {
	sh := NewTextShaper(1)
	c := NewSVGCanvas(Size{W: 400, H: 200}, sh)
	one := sh.Layout("Hello <world> & co", Font{Size: 14, Bold: true}, 0)
	c.DrawText(one, Pt(10, 20), Solid(color.NRGBA{A: 0xff}))
	out := c.Bytes()
	wellFormed(t, out)
	if got := texts(out); len(got) != 1 || got[0] != "Hello &lt;world&gt; &amp; co" {
		t.Fatalf("one line wrote %q", got)
	}
	if !strings.Contains(string(out), ` font-size="14" font-weight="bold"`) {
		t.Errorf("the font is not written:\n%s", out)
	}
	y := regexp.MustCompile(`<text x="([0-9.]+)" y="([0-9.]+)"`).FindSubmatch(out)
	if y == nil || string(y[1]) != num(10) || string(y[2]) != num(20+one.Ascent) {
		t.Errorf("the line is at %q, want x 10 and its baseline at %s", y, num(20+one.Ascent))
	}

	c = NewSVGCanvas(Size{W: 400, H: 40}, sh)
	c.DrawText(sh.Layout("two  spaces", Font{Size: 14}, 0), Pt(0, 0), Solid(color.NRGBA{A: 0xff}))
	if out := c.Bytes(); !strings.Contains(string(out), `xml:space="preserve"`) || !slices.Contains(texts(out), "two  spaces") {
		t.Errorf("a label's spaces are not kept as shaped:\n%s", out)
	}

	words := "the quick brown fox jumps over the lazy dog"
	wrapped := sh.Layout(words, Font{Size: 14}, 80)
	if len(wrapped.lines) < 2 {
		t.Fatalf("%q at 80 px wrapped to %d lines", words, len(wrapped.lines))
	}
	c = NewSVGCanvas(Size{W: 100, H: 200}, sh)
	c.DrawText(wrapped, Pt(0, 0), Solid(color.NRGBA{A: 0xff}))
	got := texts(c.Bytes())
	if len(got) != len(wrapped.lines) || strings.Join(strings.Fields(strings.Join(got, " ")), " ") != words {
		t.Errorf("a wrapped layout wrote %q; want its %d lines saying %q", got, len(wrapped.lines), words)
	}

	c = NewSVGCanvas(Size{W: 200, H: 40}, sh)
	c.DrawText(sh.LayoutRuns([]TextRun{{Text: "left", X: 0}, {Text: "right", X: 120}}, Font{Size: 12, Italic: true}), Pt(0, 0), Solid(color.NRGBA{A: 0xff}))
	out = c.Bytes()
	if got := texts(out); len(got) != 2 || got[0] != "left" || got[1] != "right" {
		t.Fatalf("a row of runs wrote %q", got)
	}
	if !strings.Contains(string(out), `<text x="120"`) || !strings.Contains(string(out), ` font-style="italic"`) {
		t.Errorf("the second run is not at its x, or the font lost its style:\n%s", out)
	}
}

// An image is written whole, as a PNG data: URI, and a part of it through a viewBox.
func TestSVGCanvasDrawsAnImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	c := NewSVGCanvas(Size{W: 40, H: 20}, nil)
	c.DrawImage(NewImage(img), Rect{W: 40, H: 20}, Rect{X: 2, W: 2, H: 2})
	out := c.Bytes()
	wellFormed(t, out)
	if s := string(out); !strings.Contains(s, `viewBox="2 0 2 2"`) || !strings.Contains(s, `href="data:image/png;base64,`) {
		t.Errorf("the image is not written:\n%s", s)
	}
}
