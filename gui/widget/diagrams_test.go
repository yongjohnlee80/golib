package widget

import (
	"context"
	"image/color"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/mermaid"
	"github.com/yongjohnlee80/golib/gui/vegalite"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

var diagramTheme = Theme{
	Text: color.NRGBA{R: 0x5b, G: 0x46, B: 0x36, A: 0xff}, Background: color.NRGBA{R: 0xe6, G: 0xd9, B: 0xb9, A: 0xff},
	Accent: color.NRGBA{R: 0x8a, G: 0x5a, B: 0x2b, A: 0xff}, Prose: gui.Font{Size: 14}, Mono: gui.Font{Size: 13},
}

func flowReq(src string) DiagramRequest {
	return DiagramRequest{Lang: "mermaid", Src: src, Width: 600, Scale: 1, Theme: diagramTheme}
}

// waitFor polls until cond holds, or fails after ten seconds: a bound on liveness, not a timing.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 1000 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A flowchart is Pending, then ready is called once, and asking again is Ready with the laid
// diagram: the cache's. Two blocks asking for the same diagram both hear of it, once each.
func TestDiagramsLayAFlowchartOnceForEveryWaiter(t *testing.T) {
	d := Diagrams(nil)
	req := flowReq("flowchart TB\n  A --> B")
	req.Scale = 2.5 // a scale of its own: holding its measurers holds no other test's jobs
	// Hold every measurer of the scale, so the job waits in AcquireMeasurer until the second asker
	// has joined it. Without this, a job with a warm measurer can finish between the two asks, and
	// the second gets the cached answer instead of joining.
	held := make([]func(), gui.MaxMeasurers)
	for i := range held {
		_, held[i] = gui.AcquireMeasurer(req.Scale)
	}
	var a, b atomic.Int32
	if ans := d.Diagram(context.Background(), req, func() { a.Add(1) }); ans.State != Pending || ans.Size.H <= 0 {
		t.Fatalf("first answer %+v, want Pending with an estimate", ans)
	}
	if ans := d.Diagram(context.Background(), req, func() { b.Add(1) }); ans.State != Pending {
		t.Fatalf("second asker got %v, want Pending (joining the job)", ans.State)
	}
	for _, release := range held {
		release()
	}
	waitFor(t, "both waiters' ready", func() bool { return a.Load() == 1 && b.Load() == 1 })
	ans := d.Diagram(context.Background(), req, func() { t.Error("a cached answer called ready") })
	if ans.State != Ready {
		t.Fatalf("asked again: %v, want Ready", ans.State)
	}
	if _, ok := ans.Pic.(*mermaid.Laid); !ok || ans.Size.W <= 0 {
		t.Errorf("the picture is a %T of %+v, want a laid diagram", ans.Pic, ans.Size)
	}
	time.Sleep(50 * time.Millisecond)
	if a.Load() != 1 || b.Load() != 1 {
		t.Errorf("ready called %d and %d times, want once each", a.Load(), b.Load())
	}
}

// A request withdrawn (its ctx cancelled: an edit, an unmount) never hears ready.
func TestAWithdrawnRequestHearsNothing(t *testing.T) {
	d := Diagrams(nil)
	ctx, cancel := context.WithCancel(context.Background())
	var fired atomic.Int32
	d.Diagram(ctx, flowReq("flowchart LR\n  X --> Y --> Z"), func() { fired.Add(1) })
	cancel()
	time.Sleep(200 * time.Millisecond)
	if fired.Load() != 0 {
		t.Errorf("a withdrawn request's ready was called %d times", fired.Load())
	}
}

type fakeFallback struct {
	asked    atomic.Int32
	declined atomic.Pointer[error] // the last request's Declined
}

func (f *fakeFallback) Diagram(_ context.Context, req DiagramRequest, _ func()) DiagramAnswer {
	f.asked.Add(1)
	f.declined.Store(&req.Declined)
	return DiagramAnswer{State: Pending}
}

// lastDeclined is the Declined of the last request the fallback was asked.
func (f *fakeFallback) lastDeclined() error {
	if p := f.declined.Load(); p != nil {
		return *p
	}
	return nil
}

// What native declines (another type, another language) is the fallback's; a malformed flowchart
// is Ready with a picture of its error; with no fallback, declined is drawn as code.
func TestTheChainFallsBackAndShowsErrors(t *testing.T) {
	fb := &fakeFallback{}
	d := Diagrams(fb)
	ready := make(chan struct{}, 1)
	other := flowReq("pie\n  \"a\": 1")
	d.Diagram(context.Background(), other, func() { ready <- struct{}{} })
	<-ready // native's job declined it: asking again goes to the fallback
	if ans := d.Diagram(context.Background(), other, func() {}); ans.State != Pending || fb.asked.Load() != 1 {
		t.Errorf("a pie: %v, fallback asked %d times; want the fallback's answer", ans.State, fb.asked.Load())
	}
	if d.Diagram(context.Background(), DiagramRequest{Lang: "plantuml", Src: "x"}, func() {}); fb.asked.Load() != 2 {
		t.Error("another language did not reach the fallback")
	}
	bad := flowReq("flowchart TB\n  A[unclosed --> B")
	d.Diagram(context.Background(), bad, func() { ready <- struct{}{} })
	<-ready
	if ans := d.Diagram(context.Background(), bad, func() {}); ans.State != Ready || ans.Pic == nil {
		t.Errorf("a malformed flowchart: %+v, want Ready with its error's picture", ans)
	}
	none := Diagrams(nil)
	none.Diagram(context.Background(), other, func() { ready <- struct{}{} })
	<-ready
	if ans := none.Diagram(context.Background(), other, func() {}); ans.State != Declined {
		t.Errorf("no fallback: %v, want Declined", ans.State)
	}
}

// The cache key follows what the picture depends on: the width (as device pixels) and the theme.
func TestTheKeyFollowsWidthAndTheme(t *testing.T) {
	a := flowReq("flowchart TB\n  A --> B")
	b := a
	b.Width = 300
	c := a
	c.Theme.Text = color.NRGBA{A: 0xff}
	if hashOf(a) == hashOf(b) || hashOf(a) == hashOf(c) {
		t.Error("a new width or theme kept the key")
	}
	if hashOf(a) != hashOf(flowReq("flowchart TB\n  A --> B")) {
		t.Error("the same request gave another key")
	}
}

// askedAt is a DiagramHost that answers nothing and keeps what it was asked.
type askedAt struct{ reqs []DiagramRequest }

func (a *askedAt) Diagram(_ Diagrammer, _ Block, req DiagramRequest) DiagramAnswer {
	a.reqs = append(a.reqs, req)
	return DiagramAnswer{State: Declined}
}

// A fence asks for its diagram at the scale of the shaper it is laid out with, the window's; a
// window moved to a screen of another scale asks again, under another key.
func TestAFenceAsksAtTheWindowsScale(t *testing.T) {
	r := NewMarkdownRenderer(WithDiagrams(Diagrams(nil)))
	lines := []string{"```mermaid", "flowchart TB", "A --> B", "```"}
	host := &askedAt{}
	for _, scale := range []float32{2, 1} {
		r.LayOut(Block{From: 0, To: 4}, lines, 400, false, gui.NewTextShaper(scale), diagramTheme, host)
	}
	if len(host.reqs) != 2 || host.reqs[0].Scale != 2 || host.reqs[1].Scale != 1 {
		t.Fatalf("asked %+v, want at scale 2, then 1", host.reqs)
	}
	if hashOf(host.reqs[0]) == hashOf(host.reqs[1]) {
		t.Error("two scales share a key: a diagram measured at one would be drawn at the other")
	}
}

// End to end: a gui Editor in its Rendered view, with the native chain, draws a flowchart fence as
// its diagram: Pending at first, then, once the layout's ready reaches the loop, a picture.
func TestTheEditorDrawsAFlowchart(t *testing.T) {
	doc := "top\n```mermaid\nflowchart LR\n  A[Start] --> B{Ok?}\n  B -->|yes| C[Done]\n```\nend"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer(WithDiagrams(Diagrams(nil)))), WithView(tuiwidget.ViewRendered),
		WithCore(tuiwidget.CoreInitialText(doc)))
	h.paint()
	picture := func() bool {
		var ok bool
		h.onLoop(func() { ok = h.e.layout.lay(h.e.layout.blockOf(1), nil).bl.Picture != nil })
		return ok
	}
	for range 200 {
		h.paint()
		if picture() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !picture() {
		t.Fatal("the flowchart fence never became a picture")
	}
	var laid bool
	h.onLoop(func() { _, laid = h.e.layout.lay(h.e.layout.blockOf(1), nil).bl.Picture.(*mermaid.Laid) })
	if !laid {
		t.Error("the picture is not the native layout")
	}
}

// scaledCanvas is a recording canvas whose text measures at another scale: a window on a HiDPI
// screen, to the editor.
type scaledCanvas struct {
	*gui.RecordingCanvas
	sh *gui.TextShaper
}

func (c scaledCanvas) Text() *gui.TextShaper { return c.sh }
func (c scaledCanvas) Scale() float32        { return c.sh.Scale() }

// A window moved to a screen of another scale lays its blocks out again, so a fence asks for its
// diagram anew at the new scale rather than keeping one measured at the old.
func TestTheEditorLaysOutAgainAtANewScale(t *testing.T) {
	doc := "top\n```mermaid\nflowchart LR\n  A --> B\n```\nend"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer(WithDiagrams(Diagrams(nil)))), WithView(tuiwidget.ViewRendered),
		WithCore(tuiwidget.CoreInitialText(doc)))
	host := func() *diagramSlot {
		var s *diagramSlot
		h.onLoop(func() { h.e.layout.lay(h.e.layout.blockOf(1), nil); s = h.e.layout.diagrams[1] })
		return s
	}
	for _, scale := range []float32{1, 2} {
		sh := gui.NewTextShaper(scale)
		h.onLoop(func() {
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
			v, _ := h.e.body.NativeView()
			v.(gui.View).Paint(scaledCanvas{rc, sh})
		})
		if s := host(); s == nil || s.key.scale != scale {
			t.Fatalf("at scale %g the fence's diagram slot is %+v, want one asked at %g", scale, s, scale)
		}
	}
}

// A vega-lite block is laid out by gui/vegalite: Pending, then Ready with the chart. One outside
// its subset is the fallback's, and one that is not JSON is Declined: its source drawn as code.
func TestDiagramsLayAChart(t *testing.T) {
	_, release := gui.AcquireMeasurer(1)
	release()
	fb := &fakeFallback{}
	d := Diagrams(fb)
	chart := func(src string) DiagramRequest {
		return DiagramRequest{Lang: "vega-lite", Src: src, Width: 600, Scale: 1, Theme: diagramTheme}
	}
	ask := func(req DiagramRequest) DiagramAnswer {
		ready := make(chan struct{}, 1)
		if a := d.Diagram(context.Background(), req, func() { ready <- struct{}{} }); a.State != Pending {
			t.Fatalf("first answer %v, want Pending", a.State)
		}
		<-ready
		return d.Diagram(context.Background(), req, func() {})
	}
	ok := chart(`{"data":{"values":[{"a":"x","b":1}]},"mark":"bar","encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative"}}}`)
	if a := ask(ok); a.State != Ready || a.Size.W <= 0 {
		t.Errorf("a bar chart: %+v, want Ready", a)
	} else if _, isChart := a.Pic.(*vegalite.Laid); !isChart {
		t.Errorf("the picture is a %T, want a laid chart", a.Pic)
	}
	if a := ask(chart(`{"data":{"values":[]},"mark":"bar","transform":[]}`)); a.State != Pending || fb.asked.Load() != 1 {
		t.Errorf("a transform: %v, fallback asked %d times; want the fallback's answer", a.State, fb.asked.Load())
	}
	if a := ask(chart(`{"mark":`)); a.State != Declined || fb.asked.Load() != 1 {
		t.Errorf("malformed JSON: %v (fallback asked %d times), want Declined", a.State, fb.asked.Load())
	}
}

// laidSize is req laid out by the native chain: its size once Ready.
func laidSize(t *testing.T, req DiagramRequest) gui.Size {
	t.Helper()
	d := Diagrams(nil)
	ready := make(chan struct{}, 1)
	d.Diagram(context.Background(), req, func() { ready <- struct{}{} })
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the diagram was never laid out")
	}
	a := d.Diagram(context.Background(), req, func() {})
	if a.State != Ready {
		t.Fatalf("laid out: %v, want Ready", a.State)
	}
	return a.Size
}

// wideLabels is a flowchart whose boxes are as wide as their labels, which are far wider in a
// monospace face than in a proportional one.
const wideLabels = "flowchart LR\n  A[iiiiiiiiiiiiiiii] --> B[llllllllllllllll]"

// inFamily is th with its prose in family.
func inFamily(th Theme, family string) Theme {
	th.Prose.Family = family
	return th
}

// A fence's diagram is measured in the family the window draws its prose in: laid out in a window
// whose prose is Go Mono, it is the size of one whose theme names Go Mono, not one in the default
// family, which the worker laying it out would otherwise use.
func TestAFencesDiagramIsMeasuredInTheWindowsProse(t *testing.T) {
	_, release := gui.AcquireMeasurer(1)
	release()
	rc := gui.NewRecordingCanvas(gui.Size{W: 600, H: 400}, gui.Size{W: 8, H: 19.2})
	rc.SetFonts(gui.Fonts{Prose: "Go Mono", Mono: "Go Mono"})
	host := &askedAt{}
	r := NewMarkdownRenderer(WithDiagrams(Diagrams(nil)))
	r.LayOut(Block{From: 0, To: 4}, append(append([]string{"```mermaid"}, strings.Split(wideLabels, "\n")...), "```"),
		600, false, rc.Text(), diagramTheme, host)
	if len(host.reqs) != 1 {
		t.Fatalf("asked %d times, want once", len(host.reqs))
	}
	mono := flowReq(wideLabels)
	mono.Theme = inFamily(diagramTheme, "Go Mono")
	sans := flowReq(wideLabels)
	sans.Theme = inFamily(diagramTheme, "Go")
	want, other := laidSize(t, mono), laidSize(t, sans)
	if want == other {
		t.Fatalf("Go Mono and Go lay out alike (%+v): the cell observes nothing", want)
	}
	if size := laidSize(t, host.reqs[0]); size != want {
		t.Errorf("laid out at %+v, want %+v (Go Mono's), not %+v (Go's)", size, want, other)
	}
}
