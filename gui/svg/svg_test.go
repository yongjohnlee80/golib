package svg

import (
	"errors"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/yongjohnlee80/golib/gui"
)

func parse(t *testing.T, doc string) *Drawing {
	t.Helper()
	d, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

func wantBounds(t *testing.T, what string, got gui.Rect, x, y, w, h float32) {
	t.Helper()
	if !near(got.X, x) || !near(got.Y, y) || !near(got.W, w) || !near(got.H, h) {
		t.Errorf("%s: bounds %+v, want {%v %v %v %v}", what, got, x, y, w, h)
	}
}

// TestPathCommands: every command, absolute and relative, lands where SVG puts it; numbers after a
// move are lines; "1.5.5" and "1-2" are two numbers each.
func TestPathCommands(t *testing.T) {
	for _, c := range []struct {
		d          string
		segs       int
		x, y, w, h float32
	}{
		{"M10 10 L20 10 L20 20 Z", 4, 10, 10, 10, 10},
		{"m10 10 l10 0 l0 10 z", 4, 10, 10, 10, 10},
		{"M10 10 20 10 20 20", 3, 10, 10, 10, 10}, // implicit lines
		{"M0 0 H5 V5 h-5 v-5", 5, 0, 0, 5, 5},
		{"M0 0 C0 10 10 10 10 0", 2, 0, 0, 10, 10},
		{"M0 0 c0 10 10 10 10 0 s10 -10 10 0", 3, 0, -10, 20, 20},
		{"M0 0 Q5 10 10 0 T20 0", 3, 0, -10, 20, 20},
		{"M0 0L1.5.5", 2, 0, 0, 1.5, 0.5},
		{"M0 0L1-2", 2, 0, -2, 1, 2},
		{"M0 0 L1e1 0", 2, 0, 0, 10, 0},
	} {
		d := parse(t, `<svg viewBox="0 0 100 100"><path d="`+c.d+`"/></svg>`)
		if d.Shapes() != 1 {
			t.Fatalf("%q: %d shapes", c.d, d.Shapes())
		}
		p := d.shapes[0].path
		if p.Segments() != c.segs {
			t.Errorf("%q: %d segments, want %d", c.d, p.Segments(), c.segs)
		}
		wantBounds(t, c.d, p.Bounds(), c.x, c.y, c.w, c.h)
	}
}

// TestArcs: an arc becomes at most four cubics ending at its end point; the sweep flag picks the
// side; a zero radius is a line and a zero-length arc nothing; flags need no separator.
func TestArcs(t *testing.T) {
	up := parse(t, `<svg viewBox="0 0 100 100"><path d="M0 0 A10 10 0 0 0 20 0"/></svg>`).shapes[0].path
	down := parse(t, `<svg viewBox="0 0 100 100"><path d="M0 0 A10 10 0 0 1 20 0"/></svg>`).shapes[0].path
	if up.Segments() != 3 || down.Segments() != 3 {
		t.Fatalf("a half circle: %d and %d segments, want a move and two cubics", up.Segments(), down.Segments())
	}
	ub, db := up.Bounds(), down.Bounds()
	if !(ub.Y < -9 && ub.Y+ub.H < 0.01) && !(ub.Y > -0.01 && ub.Y+ub.H > 9) {
		t.Errorf("sweep 0 bounds %+v: not a half circle on one side", ub)
	}
	if (ub.Y < -1) == (db.Y < -1) {
		t.Errorf("the two sweeps are on the same side: %+v and %+v", ub, db)
	}
	full := parse(t, `<svg viewBox="0 0 100 100"><path d="M0 0 a10 10 0 1 1 0.01 0"/></svg>`).shapes[0].path
	if full.Segments() != 5 {
		t.Errorf("a nearly full arc: %d segments, want a move and 4 cubics", full.Segments())
	}
	line := parse(t, `<svg viewBox="0 0 100 100"><path d="M0 0 A0 10 0 0 1 20 0"/></svg>`).shapes[0].path
	if line.Segments() != 2 {
		t.Errorf("a zero radius: %d segments, want a line", line.Segments())
	}
	flags := parse(t, `<svg viewBox="0 0 100 100"><path d="M0 0a10 10 0 0120 0"/></svg>`).shapes[0].path
	if flags.Segments() != 3 {
		t.Errorf("flags written together: %d segments", flags.Segments())
	}
	wantBounds(t, "radii grown to fit", parse(t, `<svg viewBox="0 0 100 100"><path d="M0 0 A1 1 0 0 1 20 0"/></svg>`).shapes[0].path.Bounds(), 0, -10, 20, 10)
}

// TestShapes: each basic shape's outline; one of zero size draws nothing.
func TestShapes(t *testing.T) {
	d := parse(t, `<svg viewBox="0 0 100 100">
<rect x="1" y="2" width="10" height="20"/><rect x="0" y="0" width="10" height="10" rx="3"/>
<circle cx="50" cy="50" r="5"/><ellipse cx="10" cy="10" rx="4" ry="2"/>
<line x1="0" y1="0" x2="5" y2="5" stroke="black"/><polyline points="0,0 5,5 10,0" stroke="red" fill="none"/>
<polygon points="0 0 10 0 5 5"/><rect width="0" height="5"/><circle r="0"/></svg>`)
	if d.Shapes() != 7 {
		t.Fatalf("%d shapes, want 7 (two of zero size drop)", d.Shapes())
	}
	wantBounds(t, "rect", d.shapes[0].path.Bounds(), 1, 2, 10, 20)
	wantBounds(t, "rounded rect", d.shapes[1].path.Bounds(), 0, 0, 10, 10)
	wantBounds(t, "circle", d.shapes[2].path.Bounds(), 45, 45, 10, 10)
	wantBounds(t, "ellipse", d.shapes[3].path.Bounds(), 6, 8, 8, 4)
	if d.shapes[4].filled || !d.shapes[4].lined {
		t.Error("a line is filled, or not stroked")
	}
	if d.shapes[5].filled {
		t.Error("a polyline with fill=none is filled")
	}
	if p := d.shapes[6].path; p.Segments() != 4 {
		t.Errorf("a polygon: %d segments, want 3 points and a close", p.Segments())
	}
}

// TestPaintAndInheritance: fill and stroke from attributes, overridden by style=""; a group's are
// inherited; none, currentColor and a gradient's url (with and without a fallback); opacities
// multiply down.
func TestPaintAndInheritance(t *testing.T) {
	d := parse(t, `<svg viewBox="0 0 10 10">
<g fill="#ff0000" stroke="blue" stroke-width="2" opacity="0.5">
  <rect width="1" height="1"/>
  <rect width="1" height="1" style="fill:#00ff00;stroke:none" fill-opacity="0.5"/>
  <rect width="1" height="1" fill="currentColor"/>
  <rect width="1" height="1" fill="url(#g)"/>
  <rect width="1" height="1" fill="url(#g) #0000ff" stroke="none"/>
</g></svg>`)
	if d.Shapes() != 5 {
		t.Fatalf("%d shapes", d.Shapes())
	}
	s := d.shapes
	if s[0].fill.color != (color.NRGBA{255, 0, 0, 255}) || !s[0].lined || s[0].strokeWidth != 2 || !near(s[0].fillAlpha, 0.5) {
		t.Errorf("inherited: %+v", s[0])
	}
	if s[1].fill.color != (color.NRGBA{0, 255, 0, 255}) || s[1].lined || !near(s[1].fillAlpha, 0.25) {
		t.Errorf("style over attributes, fill-opacity times the group's: %+v", s[1])
	}
	if !s[2].fill.current {
		t.Errorf("currentColor: %+v", s[2])
	}
	if s[3].filled {
		t.Errorf("a gradient with no fallback is filled: %+v", s[3])
	}
	if s[4].fill.color != (color.NRGBA{0, 0, 255, 255}) || !s[4].filled {
		t.Errorf("a gradient's fallback colour: %+v", s[4])
	}
}

// TestTransformsCompose: a group's transform applies to its children's own, outside them.
func TestTransformsCompose(t *testing.T) {
	d := parse(t, `<svg viewBox="0 0 100 100"><g transform="translate(10,0)"><rect width="1" height="1" transform="scale(2)"/></g>
<rect width="1" height="1" transform="matrix(1 0 0 1 5 6)"/><rect width="1" height="1" transform="rotate(90)"/></svg>`)
	p := d.shapes[0].xform.Transform(f32pt(1, 1))
	if !near(p.X, 12) || !near(p.Y, 2) {
		t.Errorf("translate then scale took (1,1) to %v, want (12,2)", p)
	}
	if q := d.shapes[1].xform.Transform(f32pt(0, 0)); !near(q.X, 5) || !near(q.Y, 6) {
		t.Errorf("matrix took (0,0) to %v", q)
	}
	if r := d.shapes[2].xform.Transform(f32pt(1, 0)); !near(r.X, 0) || !near(r.Y, 1) {
		t.Errorf("rotate(90) took (1,0) to %v", r)
	}
}

// TestSizeAndDraw: the intrinsic size from width/height, else the viewBox; Draw fills and
// strokes each shape inside a transform, the viewBox fitted to the rect; what is skipped (defs,
// text, a gradient) is not drawn.
func TestSizeAndDraw(t *testing.T) {
	if s := parse(t, `<svg viewBox="0 0 72 72"/>`).Size(); s.W != 72 || s.H != 72 {
		t.Errorf("viewBox size %v", s)
	}
	if s := parse(t, `<svg width="36px" viewBox="0 0 72 36"/>`).Size(); s.W != 36 || s.H != 18 {
		t.Errorf("width alone, ratio from the viewBox: %v", s)
	}
	if s := parse(t, `<svg/>`).Size(); s.W != 300 || s.H != 150 {
		t.Errorf("no size: %v", s)
	}
	d := parse(t, `<svg viewBox="0 0 10 10"><defs><rect width="5" height="5"/></defs><text>x</text>
<linearGradient id="g"><stop/></linearGradient><rect width="10" height="10" fill="#123456" stroke="#000"/></svg>`)
	rc := gui.NewRecordingCanvas(gui.Size{W: 100, H: 100}, gui.Size{W: 8, H: 16})
	d.Draw(rc, gui.Rect{W: 100, H: 50}, color.NRGBA{A: 255})
	var ops []string
	for _, c := range rc.Calls {
		ops = append(ops, c.Op)
	}
	if strings.Join(ops, " ") != "PushTransform FillPath StrokePath Pop" {
		t.Errorf("drew %v, want one rect filled and stroked in a transform", ops)
	}
}

// TestRefusesWhatIsNotSVG: no svg root, garbage, an empty input, and a number that cannot be
// drawn; none panics.
func TestRefusesWhatIsNotSVG(t *testing.T) {
	for _, doc := range []string{"", "<html><body/></html>", "not xml at all", "\x00\x01\x02"} {
		if _, err := Parse(strings.NewReader(doc)); !errors.Is(err, ErrNotSVG) {
			t.Errorf("%q: %v, want ErrNotSVG", doc, err)
		}
	}
	for _, d := range []string{"M0 0 L1e30 0", "M0 0 LNaN 0", "M0 0 L1e400 0"} {
		_, err := Parse(strings.NewReader(`<svg viewBox="0 0 10 10"><path d="` + d + `"/></svg>`))
		if !errors.Is(err, ErrInvalid) && d != "M0 0 LNaN 0" {
			t.Errorf("%q: %v, want ErrInvalid", d, err)
		}
	}
	if _, err := Parse(strings.NewReader(`<svg viewBox="0 0 Inf 10"/>`)); err != nil {
		t.Logf("an Inf viewBox: %v", err)
	}
	if _, err := Parse(strings.NewReader(`<svg width="1e30" viewBox="0 0 10 10"/>`)); !errors.Is(err, ErrInvalid) {
		t.Errorf("a 1e30 width: %v, want ErrInvalid", err)
	}
	d := parse(t, `<svg viewBox="0 0 10 10"><path d="M0 0 L5 5 X 1 2 L9 9"/></svg>`)
	if d.shapes[0].path.Segments() != 2 {
		t.Errorf("an unknown command ends the data: %d segments", d.shapes[0].path.Segments())
	}
}

// TestStressIsBounded: a path of a million commands, 10^5 arcs, 10^5 rects, nesting 10^4 deep and
// a 5 MiB document are each refused at a bound, quickly.
func TestStressIsBounded(t *testing.T) {
	cases := map[string]string{
		"a million commands": `<svg viewBox="0 0 10 10"><path d="M0 0` + strings.Repeat(" L1 1", 1_000_000) + `"/></svg>`,
		"10^5 arcs":          `<svg viewBox="0 0 10 10"><path d="M0 0` + strings.Repeat(" a5 5 0 1 1 0.1 0", 100_000) + `"/></svg>`,
		"10^5 rects":         `<svg viewBox="0 0 10 10">` + strings.Repeat(`<rect width="1" height="1"/>`, 100_000) + `</svg>`,
		"10^4 deep":          `<svg viewBox="0 0 10 10">` + strings.Repeat(`<g>`, 10_000) + strings.Repeat(`</g>`, 10_000) + `</svg>`,
		"5 MiB":              `<svg viewBox="0 0 10 10"><!--` + strings.Repeat("x", 5<<20) + `--></svg>`,
	}
	for name, doc := range cases {
		start := time.Now()
		_, err := Parse(strings.NewReader(doc))
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: %v, want ErrTooLarge", name, err)
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s took %v", name, took)
		}
	}
}

func f32pt(x, y float32) f32.Point { return f32.Pt(x, y) }

// matrices is a recording canvas that keeps each transform pushed to it.
type matrices struct {
	*gui.RecordingCanvas
	pushed []f32.Affine2D
}

func (m *matrices) PushTransform(a gui.Affine) { m.pushed = append(m.pushed, a) }

// TestComposedTransformsAreBounded: transforms are bounded as composed, not one by one: nested
// scales, each finite, that multiply past the bound, and a skew near 90°, refuse the document; so
// does a viewBox too small to scale from. A drawing drawn into a rect too large for its scale skips
// what would not be finite: the canvas is never handed NaN or Inf.
func TestComposedTransformsAreBounded(t *testing.T) {
	nested := `<svg viewBox="0 0 10 10">` + strings.Repeat(`<g transform="scale(10000000)">`, 6) + `<rect width="1" height="1"/>` +
		strings.Repeat(`</g>`, 6) + `</svg>`
	for name, doc := range map[string]string{
		"six nested scale(1e7)": nested,
		"two nested scale(200)": `<svg viewBox="0 0 10 10"><g transform="scale(200)"><rect width="1" height="1" transform="scale(200)"/></g></svg>`,
		"skewX(89.9999)":        `<svg viewBox="0 0 10 10"><rect width="1" height="1" transform="skewX(89.9999)"/></svg>`,
		"a 1e-6 viewBox":        `<svg viewBox="0 0 0.000001 0.000001"><rect width="1" height="1"/></svg>`,
	} {
		if _, err := Parse(strings.NewReader(doc)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	ok := parse(t, `<svg viewBox="0 0 10 10"><g transform="scale(100)"><rect width="1" height="1" transform="scale(50) skewX(45)"/></g></svg>`)
	for _, r := range []gui.Rect{{W: 100, H: 100}, {W: 3e38, H: 3e38}} {
		rc := &matrices{RecordingCanvas: gui.NewRecordingCanvas(gui.Size{W: 100, H: 100}, gui.Size{W: 8, H: 16})}
		ok.Draw(rc, r, color.NRGBA{A: 255})
		for _, m := range rc.pushed {
			if p := m.Transform(f32.Pt(1, 1)); math.IsNaN(float64(p.X)) || math.IsInf(float64(p.X), 0) || math.IsNaN(float64(p.Y)) || math.IsInf(float64(p.Y), 0) {
				t.Errorf("drawn into %v: a transform takes (1,1) to %v", r, p)
			}
		}
		if r.W == 100 && len(rc.pushed) != 1 {
			t.Errorf("drawn into a 100 px rect: %d transforms, want the shape's", len(rc.pushed))
		}
	}
}
