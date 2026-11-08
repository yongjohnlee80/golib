package widget

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// rowsPicture is a picture of its rows of text.
type rowsPicture []string

func (p rowsPicture) Size() tui.Size {
	w := 0
	for _, r := range p {
		w = max(w, len(r))
	}
	return tui.Size{W: w, H: len(p)}
}

func (p rowsPicture) Paint(s tui.Surface, x0, y0 int) {
	for y, r := range p {
		for x, c := range r {
			s.SetCell(x0+x, y0+y, string(c), style.Style{})
		}
	}
}

// heldLay is a lay the test answers: each request says it started (its source), is told when its
// context is cancelled, and answers the picture released for its source, cancelled or not. The view
// lays at two widths when it scrolls, so a release answers every request of its source.
type heldLay struct {
	mu        sync.Mutex
	held      map[string]*heldPicture
	started   chan string
	cancelled chan string
}

// heldPicture is a source's answer: pic, once done is closed.
type heldPicture struct {
	done   chan struct{}
	pic    CellPicture
	closed bool
}

func newHeldLay() *heldLay {
	return &heldLay{held: map[string]*heldPicture{}, started: make(chan string, 64), cancelled: make(chan string, 64)}
}

func (l *heldLay) of(src string) *heldPicture {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[src] == nil {
		l.held[src] = &heldPicture{done: make(chan struct{})}
	}
	return l.held[src]
}

// answer releases src's requests with pic.
func (l *heldLay) answer(src string, pic CellPicture) {
	h := l.of(src)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !h.closed {
		h.pic, h.closed = pic, true
		close(h.done)
	}
}

// releaseAll answers every request still held (with no picture), so no task outlives its test.
func (l *heldLay) releaseAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, h := range l.held {
		if !h.closed {
			h.closed = true
			close(h.done)
		}
	}
}

func (l *heldLay) lay(ctx context.Context, _, src string, _ func(string) int, _ int, _ SyntaxStyles) (CellPicture, error) {
	l.started <- src
	h := l.of(src)
	select {
	case <-ctx.Done():
		l.cancelled <- src
	case <-h.done:
	}
	<-h.done // answers even when cancelled: a late result the Editor must not take
	if h.pic == nil {
		return nil, ErrNoCellPicture // released by releaseAll: no picture
	}
	return h.pic, nil
}

func (l *heldLay) await(t *testing.T, c chan string, want, what string) {
	t.Helper()
	for {
		select {
		case got := <-c:
			if got == want {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s of %q", what, want)
		}
	}
}

// pictureEditor is a Rendered Markdown Editor on a running App at w×h, laying its pictures with lay.
func pictureEditor(t *testing.T, text string, lay *heldLay, w, h int) (*Editor, *internalHarness) {
	t.Helper()
	e := NewEditor(WithCore(CoreInitialText(text)))
	e.layPicture = lay.lay
	e.SetRenderer(&MarkdownCellRenderer{MarkdownBlocks: MarkdownBlocks{HideMetadata: true}})
	e.SetView(ViewRendered)
	ih := startAppInternal(t, e, w, h)
	t.Cleanup(ih.stopInternal)
	t.Cleanup(lay.releaseAll) // runs first (cleanups run last-in first-out): the App stops with no task held
	ih.syncInternal()
	return e, ih
}

// waitGrid waits for the screen to satisfy ok.
func waitGrid(t *testing.T, ih *internalHarness, what string, ok func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok(ih.tb.String()) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s:\n%s", what, ih.tb.String())
		}
		ih.syncInternal()
		time.Sleep(time.Millisecond)
	}
}

// A picture is its buffer's: another buffer shown (SetBuffer) stops the first one's request, takes
// no result of it (one answered late is dropped), and lays its own; a view switch stops a request too.
func TestAPictureIsItsBuffersOwn(t *testing.T) {
	lay := newHeldLay()
	e, ih := pictureEditor(t, "```mermaid\nA\n```\nafter", lay, 40, 10)
	lay.await(t, lay.started, "A", "request")
	ih.onLoopInternal(func() { e.Core().SetBuffer(NewBuffer(BufferText("```mermaid\nB\n```\nafter"))) })
	lay.await(t, lay.cancelled, "A", "stop")
	lay.await(t, lay.started, "B", "request")
	lay.answer("A", rowsPicture{"AAAA"}) // A's late answer
	ih.syncInternal()
	time.Sleep(50 * time.Millisecond)
	ih.syncInternal()
	if g := ih.tb.String(); strings.Contains(g, "AAAA") {
		t.Fatalf("the other buffer's picture shown:\n%s", g)
	}
	lay.answer("B", rowsPicture{"BBBB"})
	waitGrid(t, ih, "B's picture", func(g string) bool { return strings.Contains(g, "BBBB") })

	lay2 := newHeldLay()
	e2, ih2 := pictureEditor(t, "```mermaid\nC\n```", lay2, 40, 10)
	lay2.await(t, lay2.started, "C", "request")
	ih2.onLoopInternal(func() { e2.SetView(ViewRaw) })
	lay2.await(t, lay2.cancelled, "C", "stop on a view switch")
}

// A picture arriving changes the rows under the view: with the cursor off the view (scrolled away),
// the view keeps its top line, its rows above the view clamped to what the line's block has now;
// with the cursor on the view, the cursor is revealed again on the picture.
func TestAPicturesArrivalKeepsTheView(t *testing.T) {
	long := "```mermaid\n" + strings.Repeat("x ", 300) + "\n```\nafter" + strings.Repeat("\nline", 20)
	lay := newHeldLay()
	e, ih := pictureEditor(t, long, lay, 40, 10)
	src := strings.Repeat("x ", 300)
	lay.await(t, lay.started, src, "request")
	ih.onLoopInternal(func() { e.cells.ScrollLines(12) }) // inside the source's long line, the cursor above the view
	ih.syncInternal()
	lay.answer(src, rowsPicture{"PIC0", "PIC1"})
	waitGrid(t, ih, "the picture", func(g string) bool { return strings.Contains(g, "PIC") })
	if row0 := strings.Split(ih.tb.String(), "\n")[0]; !strings.HasPrefix(row0, "PIC1") {
		t.Errorf("scrolled into the fence's source, the view's first row after the picture %q, want PIC1 (its block's last row)", row0)
	}

	lay2 := newHeldLay()
	e2, ih2 := pictureEditor(t, long, lay2, 40, 10)
	lay2.await(t, lay2.started, src, "request")
	ih2.onLoopInternal(func() { e2.Core().SetLine(1, 590) }) // the cursor deep in the source, on the view
	ih2.syncInternal()
	lay2.answer(src, rowsPicture{"PIC0", "PIC1"})
	waitGrid(t, ih2, "the picture", func(g string) bool { return strings.Contains(g, "PIC") })
	var ok bool
	var y int
	ih2.onLoopInternal(func() { _, y, ok = e2.Cursor() })
	if row := strings.Split(ih2.tb.String(), "\n"); !ok || !strings.HasPrefix(row[y], "PIC0") {
		t.Errorf("the cursor on the view before the picture: after it, shown %v on row %d, want on the picture's first row:\n%s", ok, y, ih2.tb.String())
	}
}

// A picture's whole fence is one fold that shows: no row of its source (not its language), its
// lines one fold from its opener to its closer, the caret on any of them on the picture's first row,
// a click on the picture its first line; dd takes the whole fence, the prose after it kept, and u
// brings it back.
func TestAPicturesFenceIsOneVisibleFold(t *testing.T) {
	const doc = "```mermaid\nA\nB\n```\nafter"
	lay := newHeldLay()
	e, ih := pictureEditor(t, doc, lay, 40, 10)
	lay.await(t, lay.started, "A\nB", "request")
	lay.answer("A\nB", rowsPicture{"PIC0", "PIC1"})
	waitGrid(t, ih, "the picture", func(g string) bool { return strings.Contains(g, "PIC1") })
	if g := ih.tb.String(); strings.Contains(g, "mermaid") || !strings.HasPrefix(g, "PIC0") {
		t.Errorf("want the picture alone where its fence is, no language row:\n%s", g)
	}
	for ln := range 4 {
		var from, to int
		var hidden, ok bool
		ih.onLoopInternal(func() { from, to, hidden, ok = e.cells.FoldOf(ln) })
		if !ok || from != 0 || to != 4 || hidden {
			t.Errorf("FoldOf(%d) = [%d, %d) hidden %v ok %v, want [0, 4) shown", ln, from, to, hidden, ok)
		}
	}
	var x, y int
	var shown bool
	ih.onLoopInternal(func() { e.Core().SetLine(2, 0); x, y, shown = e.Cursor() })
	if !shown || x != 0 || y != 0 {
		t.Errorf("the caret on the fence's body at (%d, %d) %v, want the picture's first row (0, 0)", x, y, shown)
	}
	ih.onLoopInternal(func() { e.Core().SetLine(4, 0) })
	if err := ih.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 2, Y: 1}); err != nil {
		t.Fatal(err)
	}
	ih.syncInternal()
	var ln int
	ih.onLoopInternal(func() { ln, _ = e.Core().Line() })
	if ln != 0 {
		t.Errorf("a click on the picture landed on line %d, want the fence's first, 0", ln)
	}
	var v string
	ih.onLoopInternal(func() {
		e.Core().SetLine(2, 0)
		e.Core().HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: 'd'})
		e.Core().HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: 'd'})
		v = e.Core().Value()
	})
	if v != "after" {
		t.Errorf("dd on the picture left %q, want the prose after it", v)
	}
	ih.onLoopInternal(func() {
		e.Core().HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: 'u'})
		v = e.Core().Value()
	})
	if v != doc {
		t.Errorf("u after dd: %q, want the note back", v)
	}
}

// An edit touching a fence whose picture is laying stops the request (its late answer is not
// taken), and the edited fence asks again once it is drawn rendered.
func TestAnEditStopsItsFencesPicture(t *testing.T) {
	lay := newHeldLay()
	e, ih := pictureEditor(t, "```mermaid\nA\n```\nafter", lay, 40, 10)
	lay.await(t, lay.started, "A", "request")
	key := func(c rune) { e.Core().HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: c}) }
	ih.onLoopInternal(func() {
		e.Core().SetLine(1, 0)
		key('i')
		key('X') // typing in the fence: its block is its source, so no picture is asked for
	})
	lay.await(t, lay.cancelled, "A", "stop on an edit")
	lay.answer("A", rowsPicture{"AAAA"}) // A's late answer
	ih.onLoopInternal(func() { e.Core().HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}) })
	lay.await(t, lay.started, "XA", "request of the edited fence")
	ih.syncInternal()
	if g := ih.tb.String(); strings.Contains(g, "AAAA") {
		t.Fatalf("the edited fence drawn with its old source's picture:\n%s", g)
	}
	lay.answer("XA", rowsPicture{"XXXX"})
	waitGrid(t, ih, "the edited fence's picture", func(g string) bool { return strings.Contains(g, "XXXX") })
}

// Two pictures arriving before one layout: whether the cursor was on the view is taken before the
// first changes the rows, and kept through the second, so the cursor is revealed though the second
// arrives when the rows under the view have changed and it is off it.
func TestPicturesArrivingTogetherKeepTheReveal(t *testing.T) {
	src := strings.Repeat("x ", 300)
	doc := "```mermaid\n" + src + "\n```\nmid\n```mermaid\nB\n```\nafter" + strings.Repeat("\nline", 20)
	lay := newHeldLay()
	e, ih := pictureEditor(t, doc, lay, 40, 10)
	lay.await(t, lay.started, src, "request")
	lay.await(t, lay.started, "B", "request")
	ih.onLoopInternal(func() { e.Core().SetLine(1, 590) }) // the cursor deep in the long source, on the view
	ih.syncInternal()
	ih.onLoopInternal(func() {
		// every pending request answered at once, before a layout: the long fence's first (it changes
		// the rows under the cursor), then the rest
		var ats []pictureAt
		for at, s := range e.pics {
			if s.pending {
				ats = append(ats, at)
			}
		}
		slices.SortFunc(ats, func(a, b pictureAt) int { return cmp.Or(a.from-b.from, a.cols-b.cols) })
		for _, at := range ats { // through the Editor's own handling, as the loop delivers them
			e.HandleEvent(tui.TaskResult{Owner: e.Context().ID(), ID: e.pics[at].task, Value: rowsPicture{"PIC0", "PIC1"}})
		}
	})
	waitGrid(t, ih, "the pictures drawn", func(g string) bool { return strings.Contains(g, "PIC") })
	var ok bool
	ih.onLoopInternal(func() { _, _, ok = e.Cursor() })
	if !ok {
		t.Errorf("two pictures before one layout: the cursor, on the view before them, is off it after:\n%s", ih.tb.String())
	}
}

// startAppHeld is startAppInternal with every frame after the first held back an hour: input is
// routed with no frame between it and what came before it.
func startAppHeld(t *testing.T, root tui.Component, w, h int) *internalHarness {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	ih := &internalHarness{t: t, app: app, tb: tb, res: make(chan error, 1), cxl: cancel}
	go func() { ih.res <- app.Run(ctx) }()
	ih.syncInternal()
	return ih
}

// A picture's arrival settles the layout before the next input: with no frame after it (frames held),
// a click lands on the rows the picture and the revealed view put under it, not on the old offset.
// The cursor is deep in the fence's source (the view scrolled to it) when the picture arrives; it is
// revealed on the picture, so the click on the view's first row is the fence's first line.
func TestAPicturesArrivalSettlesBeforeTheNextInput(t *testing.T) {
	src := strings.Repeat("x ", 300)
	e := NewEditor(WithCore(CoreInitialText("```mermaid\n" + src + "\n```\nafter" + strings.Repeat("\nline", 20))))
	lay := newHeldLay()
	e.layPicture = lay.lay
	e.SetRenderer(&MarkdownCellRenderer{MarkdownBlocks: MarkdownBlocks{HideMetadata: true}})
	e.SetView(ViewRendered)
	e.Core().SetLine(1, 590) // deep in the long line: the first frame reveals it, rows into the fence
	ih := startAppHeld(t, e, 40, 10)
	t.Cleanup(ih.stopInternal)
	t.Cleanup(lay.releaseAll)
	lay.await(t, lay.started, src, "request")
	var gen uint64
	ih.onLoopInternal(func() { gen = e.picGen })
	lay.answer(src, rowsPicture{"PIC0", "PIC1"})
	deadline := time.Now().Add(3 * time.Second)
	for { // the picture handled on the loop (a frame stays held)
		var now uint64
		ih.onLoopInternal(func() { now = e.picGen })
		if now != gen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the picture's result was not handled")
		}
		time.Sleep(time.Millisecond)
	}
	if err := ih.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	var ln int
	for deadline := time.Now().Add(3 * time.Second); ; { // the click routed (input and Updates are separate lanes)
		ih.onLoopInternal(func() { ln, _ = e.Core().Line() })
		if ln != 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if ln != 0 {
		t.Errorf("a click on the view's first row right after the picture landed on line %d, want 0 (the picture's fence)", ln)
	}
}
