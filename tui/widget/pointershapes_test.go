package widget_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// shapeHost is a TestBackend that shapes the pointer, keeping the last frame's runs.
type shapeHost struct {
	*tui.TestBackend
	mu   sync.Mutex
	last []tui.PointerShapePlacement
	n    int
	app  *tui.App
}

func (b *shapeHost) PlacePointerShapes(ps []tui.PointerShapePlacement) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last, b.n = append([]tui.PointerShapePlacement(nil), ps...), b.n+1
}

// at is cell (x, y)'s shape in the last frame: the last run over it, else the default.
func (b *shapeHost) at(x, y int) tui.PointerShape {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := tui.PointerDefault
	for _, p := range b.last {
		if y == p.Y && x >= p.X && x < p.X+p.W {
			s = p.Shape
		}
	}
	return s
}

// runShaped runs root on a shape host, and waits for its first shaped frame.
func runShaped(t *testing.T, root tui.Component, w, h int) *shapeHost {
	t.Helper()
	return runShapedOn(t, root, w, h, tui.Capabilities{})
}

// runShapedOn is runShaped on a host reporting caps.
func runShapedOn(t *testing.T, root tui.Component, w, h int, caps tui.Capabilities) *shapeHost {
	t.Helper()
	sh := &shapeHost{TestBackend: tui.NewTestBackend(w, h, tui.WithTestCapabilities(caps))}
	app := tui.NewApp(root, tui.WithBackend(sh), tui.WithMinFrameInterval(0))
	sh.app = app
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for {
		sh.mu.Lock()
		n := sh.n
		sh.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no frame's pointer shapes")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return sh
}

// expect checks each cell's shape, waiting for the frame that has them all.
func expect(t *testing.T, sh *shapeHost, cells map[[2]int]tui.PointerShape) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var bad []string
		for c, want := range cells {
			if got := sh.at(c[0], c[1]); got != want {
				bad = append(bad, "("+itoa(c[0])+", "+itoa(c[1])+"): "+itoa(int(got))+", want "+itoa(int(want)))
			}
		}
		if len(bad) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pointer shapes: %s", strings.Join(bad, "; "))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func nLines(n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("line")
	}
	return b.String()
}

// The editor's text is an I-beam; its gutter and its scroll indicator keep the arrow.
func TestTheEditorsTextIsAnIBeam(t *testing.T) {
	e := widget.NewEditor()
	e.SetLineNumbers(true)
	e.SetValue(nLines(12)) // more lines than rows: the scroll indicator shows
	sh := runShaped(t, e, 20, 5)
	// the gutter is 4 digits and a gap of 2: the text from column 6, the indicator at 19
	expect(t, sh, map[[2]int]tui.PointerShape{
		{0, 0}: tui.PointerDefault, {5, 2}: tui.PointerDefault,
		{6, 0}: tui.PointerText, {18, 4}: tui.PointerText,
		{19, 0}: tui.PointerDefault,
	})
}

// A text area's text, and a text input's, are an I-beam; the area's scroll indicator is not.
func TestTextAreasAndInputsAreAnIBeam(t *testing.T) {
	a := widget.NewTextArea()
	a.SetValue(nLines(10))
	sh := runShaped(t, a, 20, 3)
	expect(t, sh, map[[2]int]tui.PointerShape{{0, 0}: tui.PointerText, {18, 2}: tui.PointerText, {19, 1}: tui.PointerDefault})

	in := widget.NewTextInput()
	in.SetValue("find")
	sh = runShaped(t, in, 12, 1)
	expect(t, sh, map[[2]int]tui.PointerShape{{0, 0}: tui.PointerText, {11, 0}: tui.PointerText})
}

// A split's divider is a resize arrow across it; its panes are not.
func TestASplitsDividerIsAResizeArrow(t *testing.T) {
	s := widget.NewSplit(widget.Horizontal, widget.NewText(""), widget.NewText(""), widget.WithRatio(0.5))
	sh := runShaped(t, s, 21, 4)
	var a int
	deadline := time.Now().Add(5 * time.Second)
	for a == 0 && time.Now().Before(deadline) {
		a, _, _ = s.Cells()
		time.Sleep(5 * time.Millisecond)
	}
	expect(t, sh, map[[2]int]tui.PointerShape{{a, 0}: tui.PointerResizeEW, {a, 3}: tui.PointerResizeEW, {a + 2, 1}: tui.PointerDefault})

	v := widget.NewSplit(widget.Vertical, widget.NewText(""), widget.NewText(""), widget.WithRatio(0.5))
	sh = runShaped(t, v, 10, 9)
	deadline = time.Now().Add(5 * time.Second)
	for a = 0; a == 0 && time.Now().Before(deadline); {
		a, _, _ = v.Cells()
		time.Sleep(5 * time.Millisecond)
	}
	expect(t, sh, map[[2]int]tui.PointerShape{{0, a}: tui.PointerResizeNS, {9, a}: tui.PointerResizeNS, {4, a + 1}: tui.PointerDefault})
}

// A link is a hand; the text around it, and the page past it, are not.
func TestAnHTMLViewsLinkIsAHand(t *testing.T) {
	v := widget.NewHTMLView()
	v.SetHTML([]byte(`<p>see <a href="b.md">the link</a> here</p>`))
	sh := runShaped(t, v, 40, 4)
	expect(t, sh, map[[2]int]tui.PointerShape{
		{1, 0}: tui.PointerDefault, {4, 0}: tui.PointerHand, {11, 0}: tui.PointerHand, {13, 0}: tui.PointerDefault,
		{30, 0}: tui.PointerDefault, {4, 2}: tui.PointerDefault,
	})
}

// A resizable's grips are their drag's arrows; what they resize is not.
func TestAResizablesGripsAreResizeArrows(t *testing.T) {
	r := widget.NewResizable(widget.NewText("body"), widget.WithHandles(widget.HandleRight, widget.HandleBottom, widget.HandleBottomRight))
	sh := runShaped(t, r, 12, 6)
	var ew, ns, corner, plain int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ew, ns, corner, plain = 0, 0, 0, 0
		for y := range 6 {
			for x := range 12 {
				switch sh.at(x, y) {
				case tui.PointerResizeEW:
					ew++
				case tui.PointerResizeNS:
					ns++
				case tui.PointerResizeNWSE:
					corner++
				default:
					plain++
				}
			}
		}
		if ew > 0 && ns > 0 && corner > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ew == 0 || ns == 0 || corner == 0 || sh.at(0, 0) != tui.PointerDefault {
		t.Fatalf("grips: %d EW, %d NS, %d corner cells, (0, 0) %v; want each grip shaped and the body plain", ew, ns, corner, sh.at(0, 0))
	}
}

// A list's row pointer is over the rows that hold an item: not the scroll indicator, not the
// space past its last row, and nothing at all unless it is asked for.
func TestAListsRowPointerIsOverItsItems(t *testing.T) {
	src := func(n int) widget.ListOption[string] {
		items := make([]string, n)
		for i := range items {
			items[i] = "item"
		}
		return widget.WithItems(items, func(s string) string { return s })
	}
	short := widget.NewList(src(3), widget.WithListRowPointer[string](tui.PointerHand))
	sh := runShaped(t, short, 20, 5)
	expect(t, sh, map[[2]int]tui.PointerShape{{0, 0}: tui.PointerHand, {19, 2}: tui.PointerHand, {0, 3}: tui.PointerDefault, {5, 4}: tui.PointerDefault})

	long := widget.NewList(src(10), widget.WithListRowPointer[string](tui.PointerHand))
	sh = runShaped(t, long, 20, 5)
	expect(t, sh, map[[2]int]tui.PointerShape{{0, 0}: tui.PointerHand, {18, 4}: tui.PointerHand, {19, 0}: tui.PointerDefault})

	plain := widget.NewList(src(3))
	sh = runShaped(t, plain, 20, 5)
	time.Sleep(50 * time.Millisecond)
	if s := sh.at(0, 0); s != tui.PointerDefault {
		t.Errorf("a list not asked for a row pointer shows %v", s)
	}
}

// While an editor's rendered document shows in the text's place, the editor gives no I-beam of its
// own; back on the text, it does.
func TestTheEditorGivesItsIBeamUpToItsDocument(t *testing.T) {
	h, sh, ed, _ := csvEditor(t, "name,n\nann,1\n")
	var text, doc []tui.PointerRegion
	h.onLoop(func() { text = ed.PointerShapes(nil) })
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	h.onLoop(func() { doc = ed.PointerShapes(nil) })
	if len(text) != 1 || text[0].Shape != tui.PointerText {
		t.Errorf("on the text: %+v, want the I-beam", text)
	}
	if len(doc) != 0 {
		t.Errorf("with the document shown: %+v, want none of the editor's", doc)
	}
}

// A list's row pointer changed at runtime shows with the next frame, and so does its clearing: the
// change repaints.
func TestAListsRowPointerChangesWithTheNextFrame(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b"}, func(s string) string { return s }), widget.WithListRowPointer[string](tui.PointerHand))
	sh := runShaped(t, l, 20, 4)
	expect(t, sh, map[[2]int]tui.PointerShape{{0, 0}: tui.PointerHand})
	sh.app.Update(func() { l.SetRowPointerShape(tui.PointerText) })
	expect(t, sh, map[[2]int]tui.PointerShape{{0, 0}: tui.PointerText, {0, 1}: tui.PointerText})
	sh.app.Update(func() { l.SetRowPointerShape(tui.PointerDefault) })
	expect(t, sh, map[[2]int]tui.PointerShape{{0, 0}: tui.PointerDefault, {0, 1}: tui.PointerDefault})
}
