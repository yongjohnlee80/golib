package widget

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// fakeBaker records and draws on demand, counting its draws; err, when set, is every draw's
// answer. Its recording's size is its next draw's. release is what OnRelease registered.
type fakeBaker struct {
	mu      sync.Mutex
	bakes   int
	err     error
	px      image.Point
	scale   float32
	release []func()
}

func (f *fakeBaker) Record(v gui.View, size gui.Size, scale float32) (*gui.Recording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.px, f.scale = gui.BakedPixels(size, scale), scale
	return new(gui.Recording), nil
}

func (f *fakeBaker) Draw(*gui.Recording) (gui.Baked, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bakes++
	if f.err != nil {
		return gui.Baked{}, f.err
	}
	return gui.Baked{Image: gui.NewImage(image.NewRGBA(image.Rectangle{Max: f.px})), Pixels: f.px, Scale: f.scale}, nil
}

func (f *fakeBaker) OnRelease(fn func()) {
	f.mu.Lock()
	f.release = append(f.release, fn)
	f.mu.Unlock()
}

func (f *fakeBaker) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.bakes }

// withBaker makes every canvas a window's with baker b, on a cache of its own of limit pixels.
func withBaker(t *testing.T, b pictureBaker, limit int) *pictureCache {
	t.Helper()
	oldOf, oldCache := bakerOf, pictures
	bakerOf = func(gui.Canvas) pictureBaker { return b }
	pictures = newPictureCache(limit)
	t.Cleanup(func() { bakerOf, pictures = oldOf, oldCache })
	return pictures
}

// paintedFrom is what a picture painted on a fresh recording: "vector" (its own calls) or "image".
func paintedFrom(p gui.View) string {
	rc := gui.NewRecordingCanvas(gui.Size{W: 40, H: 20}, gui.Size{W: 1, H: 1})
	p.Paint(rc)
	for _, c := range rc.Calls {
		if c.Op == "DrawImage" {
			return "image"
		}
	}
	return "vector"
}

var vectorPic = gui.ViewFunc(func(c gui.Canvas) { c.FillRect(gui.Rect{W: 10, H: 10}, gui.Solid(color.NRGBA{R: 0xff, A: 0xff})) })

// A picture is drawn from its paths until its image is drawn, then from the image: one bake, the
// view woken once, every later paint the image's.
func TestAPictureIsDrawnFromItsImageOnceBaked(t *testing.T) {
	fb := &fakeBaker{}
	withBaker(t, fb, 1<<20)
	woke := make(chan struct{}, 4)
	p := newCachedPicture(vectorPic, gui.Size{W: 40, H: 20}, func() { woke <- struct{}{} })
	if got := paintedFrom(p); got != "vector" {
		t.Fatalf("before its bake the picture is drawn from its %s", got)
	}
	<-woke
	for range 3 {
		if got := paintedFrom(p); got != "image" {
			t.Fatalf("after its bake the picture is drawn from its %s", got)
		}
	}
	if n := fb.count(); n != 1 {
		t.Fatalf("%d bakes, want one", n)
	}
}

// A picture's image is its own, at a scale and a generation of the window's fonts: two pictures
// of one request (two Diagrammers might draw it two ways) are two images, and another scale, font
// generation or window is another image.
func TestAPicturesImageIsItsOwnAtAScaleAndFonts(t *testing.T) {
	fb := &fakeBaker{}
	withBaker(t, fb, 1<<20)
	woke := make(chan struct{}, 4)
	a := newCachedPicture(vectorPic, gui.Size{W: 40, H: 20}, func() { woke <- struct{}{} })
	b := newCachedPicture(vectorPic, gui.Size{W: 40, H: 20}, func() { woke <- struct{}{} })
	paintedFrom(a)
	<-woke
	if paintedFrom(b) != "vector" {
		t.Fatal("another picture of the same request is drawn from the first one's image")
	}
	<-woke
	if n := fb.count(); n != 2 {
		t.Fatalf("%d bakes for two pictures, want two", n)
	}
	pc := pictures
	base := bakeKey{baker: fb, pic: a.id, scale: 1, fonts: 1}
	pc.mu.Lock()
	pc.put(base, gui.Baked{Pixels: image.Pt(4, 4), Scale: 1})
	pc.mu.Unlock()
	others := map[string]bakeKey{}
	k := base
	k.pic = b.id
	others["picture"] = k
	k = base
	k.scale = 1.5
	others["scale"] = k
	k = base
	k.fonts = 2
	others["fonts"] = k
	k = base
	k.baker = &fakeBaker{}
	others["window"] = k
	if _, ok := pc.get(base); !ok {
		t.Fatal("the key put is not found")
	}
	for what, other := range others {
		if _, ok := pc.get(other); ok {
			t.Errorf("another %s finds the image", what)
		}
	}
}

// A window closed lets go of everything of its: its images, and its baker, so the window is not
// kept; a draw that ends after it is not kept either.
func TestAClosedWindowsPicturesAreLetGo(t *testing.T) {
	fb := &fakeBaker{}
	pc := withBaker(t, fb, 1<<20)
	woke := make(chan struct{}, 4)
	p := newCachedPicture(vectorPic, gui.Size{W: 40, H: 20}, func() { woke <- struct{}{} })
	paintedFrom(p)
	<-woke
	fb.mu.Lock()
	release := append([]func(){}, fb.release...)
	fb.mu.Unlock()
	if len(release) != 1 {
		t.Fatalf("the cache asked to hear of the window's release %d times, want once", len(release))
	}
	for _, fn := range release {
		fn()
	}
	pc.mu.Lock()
	left := len(pc.baked) + len(pc.pending) + len(pc.skip) + len(pc.dead) + len(pc.watched)
	pixels := pc.pixels
	pc.mu.Unlock()
	if left != 0 || pixels != 0 {
		t.Fatalf("after the window closed the cache holds %d entries of %d pixels, want none", left, pixels)
	}
	pc.mu.Lock() // a draw that ends now, its window gone
	pc.pending[bakeKey{baker: fb, pic: p.id, scale: 1}] = true
	pc.mu.Unlock()
	pc.enqueue(bakeJob{key: bakeKey{baker: fb, pic: p.id, scale: 1}, rec: new(gui.Recording)})
	waitFor(t, "the late draw", func() bool { pc.mu.Lock(); defer pc.mu.Unlock(); return len(pc.pending) == 0 })
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if len(pc.baked) != 0 {
		t.Fatal("a draw that ended after its window closed was kept")
	}
}

// The cache keeps its limit of pixels, the least recently painted going first.
func TestTheCacheKeepsItsLimitLeastRecentlyPaintedFirst(t *testing.T) {
	pc := withBaker(t, &fakeBaker{}, 100)
	ids := map[string]uint64{"a": 1, "b": 2, "c": 3}
	key := func(src string) bakeKey { return bakeKey{pic: ids[src], scale: 1} }
	put := func(src string) {
		pc.mu.Lock()
		pc.put(key(src), gui.Baked{Pixels: image.Pt(8, 5), Scale: 1}) // 40 pixels each
		pc.mu.Unlock()
	}
	put("a")
	put("b")
	pc.get(key("a")) // a painted after b
	put("c")         // 120 pixels: the least recently painted, b, goes
	for src, want := range map[string]bool{"a": true, "b": false, "c": true} {
		if _, ok := pc.get(key(src)); ok != want {
			t.Errorf("%s kept: %v, want %v", src, ok, want)
		}
	}
	if pc.pixels != 80 {
		t.Errorf("%d pixels kept, want 80", pc.pixels)
	}
}

// With no headless GPU, a window's pictures are drawn from their paths, its baker asked once; a
// picture larger than the cache is never baked at all.
func TestWithNoGPUOrTooLargeAPictureStaysVector(t *testing.T) {
	fb := &fakeBaker{err: gui.ErrNoGPU}
	withBaker(t, fb, 1<<20)
	a := newCachedPicture(vectorPic, gui.Size{W: 40, H: 20}, nil)
	b := newCachedPicture(vectorPic, gui.Size{W: 40, H: 20}, nil)
	paintedFrom(a)
	waitFor(t, "the first bake", func() bool { return fb.count() == 1 })
	waitFor(t, "the baker marked without a GPU", func() bool {
		pictures.mu.Lock()
		defer pictures.mu.Unlock()
		return pictures.dead[fb]
	})
	for range 3 {
		if paintedFrom(a) != "vector" || paintedFrom(b) != "vector" {
			t.Fatal("with no GPU a picture is drawn from an image")
		}
	}
	if n := fb.count(); n != 1 {
		t.Fatalf("with no GPU the baker was asked %d times, want once", n)
	}

	big := &fakeBaker{}
	withBaker(t, big, 100)
	huge := newCachedPicture(vectorPic, gui.Size{W: 40, H: 20}, nil) // 800 pixels
	for range 3 {
		if paintedFrom(huge) != "vector" {
			t.Fatal("a picture past the limit is drawn from an image")
		}
	}
	if n := big.count(); n != 0 {
		t.Fatalf("a picture past the limit was baked %d times", n)
	}
}

// readyPic is d's Ready picture for req, waiting out its Pending.
func readyPic(t *testing.T, d Diagrammer, req DiagramRequest) DiagramAnswer {
	t.Helper()
	ready := make(chan struct{}, 1)
	a := d.Diagram(context.Background(), req, func() { ready <- struct{}{} })
	if a.State == Pending {
		<-ready
		a = d.Diagram(context.Background(), req, func() {})
	}
	if a.State != Ready || a.Pic == nil {
		t.Fatalf("%s: %v, want Ready with a picture", req.Lang, a.State)
	}
	return a
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// A chart and a diagram drawn from their images are drawn as their paths draw them, on their page,
// in sepia, dark and light, at scales 1, 1.5 and 2: every channel of every pixel within two levels.
func TestACachedChartIsDrawnAsItsPathsDrawIt(t *testing.T) {
	pb := gui.NewBackend().NewPictureBaker()
	defer pb.Release()
	themes := map[string]Theme{
		"sepia": {Background: rgb(0xe6d9b9), Text: rgb(0x5b4636), Accent: rgb(0x8a5a2b), Muted: rgb(0x8c7656), CodeBackground: rgb(0xdacca9)},
		"dark":  {Background: rgb(0x1c1c1c), Text: rgb(0xd0d0d0), Accent: rgb(0x87afd7), Muted: rgb(0x8a8a8a), CodeBackground: rgb(0x303030)},
		"light": {Background: rgb(0xf6f2e7), Text: rgb(0x2e2a24), Accent: rgb(0x2f5f8f), Muted: rgb(0x7a7163), CodeBackground: rgb(0xebe6d9)},
	}
	sources := []DiagramRequest{
		{Lang: "vega-lite", Src: `{"data":{"values":[{"a":"Apples","b":28},{"a":"Bread","b":55},{"a":"Cheese","b":43}]},` +
			`"mark":"bar","encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative"}}}`},
		{Lang: "mermaid", Src: "flowchart LR\n  Start --> Middle --> End"},
	}
	d := Diagrams(nil)
	for name, th := range themes {
		for _, scale := range []float32{1, 1.5, 2} {
			for _, src := range sources {
				req := src
				req.Width, req.Scale, req.Theme = 400, scale, th
				a := readyPic(t, d, req)
				onPage := func(draw func(c gui.Canvas)) gui.View {
					return gui.ViewFunc(func(c gui.Canvas) {
						c.FillRect(gui.Rect{W: a.Size.W, H: a.Size.H}, gui.Solid(th.Background))
						draw(c)
					})
				}
				what := fmt.Sprintf("%s %s at %v", name, req.Lang, scale)
				vector, err := pb.Bake(onPage(a.Pic.Paint), a.Size, scale)
				if errors.Is(err, gui.ErrNoGPU) {
					t.Skipf("no headless GPU here: %v", err)
				}
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				img, err := pb.Bake(a.Pic, a.Size, scale)
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				cached, err := pb.Bake(onPage(func(c gui.Canvas) { gui.DrawBaked(c, img, gui.Point{}) }), a.Size, scale)
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				drawn := 0 // pixels not the page's: the chart is there to compare
				page := th.Background
				for i := 0; i+3 < len(cached.RGBA.Pix); i += 4 {
					if p := cached.RGBA.Pix[i : i+3]; p[0] != page.R || p[1] != page.G || p[2] != page.B {
						drawn++
					}
				}
				if drawn < 100 {
					t.Fatalf("%s: %d pixels of the cached drawing are not its page: the chart was not drawn", what, drawn)
				}
				off, worst := 0, 0
				for i := range vector.RGBA.Pix {
					d := int(vector.RGBA.Pix[i]) - int(cached.RGBA.Pix[i])
					d = max(d, -d)
					worst = max(worst, d)
					if d > 2 {
						off++
					}
				}
				if off > 0 {
					t.Errorf("%s: %d channels differ by more than 2 levels (at most %d)", what, off, worst)
				}
			}
		}
	}
}

// Scrolling a page of diagrams down and back draws no image again: after the first pass, each
// diagram's image is drawn once, and the pass back finds them all.
func TestScrollingAPageOfDiagramsBakesEachOnce(t *testing.T) {
	fb := &fakeBaker{}
	withBaker(t, fb, 1<<24)
	var doc string
	for i := range 4 {
		doc += fmt.Sprintf("# Part %d\n\n```mermaid\nflowchart LR\n  A%d --> B%d\n```\n\n", i, i, i)
		for j := range 30 {
			doc += fmt.Sprintf("Line %d of part %d.\n\n", j, i)
		}
	}
	h := startEditor(t, 60, 20, WithCore(editor.CoreInitialText(doc)),
		WithRenderer(NewMarkdownRenderer(WithDiagrams(Diagrams(nil)))), WithView(editor.ViewRendered))
	pass := func() {
		for range 200 {
			var moved bool
			h.onLoop(func() { moved = h.e.layout.scrollBy(cellH * 6) })
			h.paint()
			if !moved {
				break
			}
		}
		for range 200 {
			var moved bool
			h.onLoop(func() { moved = h.e.layout.scrollBy(-cellH * 6) })
			h.paint()
			if !moved {
				break
			}
		}
	}
	waitFor(t, "every diagram laid and baked", func() bool {
		pass()
		return fb.count() >= 4
	})
	pass() // a second warm pass: every image found
	n := fb.count()
	pass()
	pass()
	if got := fb.count(); got != n || n != 4 {
		t.Fatalf("bakes: %d after warming, %d after two more passes; want 4, then none", n, got)
	}
}
