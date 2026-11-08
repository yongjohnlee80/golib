package widget

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// editorRun is a Rendered Markdown Editor on a w × h test terminal with caps, running.
type editorRun struct {
	t   *testing.T
	tb  *tui.TestBackend
	app *tui.App
	e   *Editor
}

func runImageEditor(t *testing.T, text string, w, h int, caps tui.Capabilities, opts ...EditorOption) *editorRun {
	t.Helper()
	e := NewEditor(append([]EditorOption{WithCore(CoreInitialText(text))}, opts...)...)
	e.SetRenderer(&MarkdownCellRenderer{MarkdownBlocks: MarkdownBlocks{HideMetadata: true}})
	e.SetView(ViewRendered)
	tb := tui.NewTestBackend(w, h, tui.WithTestCapabilities(caps))
	app := tui.NewApp(e, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &editorRun{t, tb, app, e}
}

// waitFor waits, up to five seconds, for ok of the screen and the images placed.
func (r *editorRun) waitFor(what string, ok func(screen string, imgs map[uint32]tui.ImagePlacement) bool) {
	r.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if r.tb.Flushes() > 0 && ok(r.tb.String(), r.tb.Images()) {
			return
		}
	}
	r.t.Fatalf("%s never came: images %+v\n%s", what, r.tb.Images(), r.tb.String())
}

func (r *editorRun) onLoop(fn func()) {
	r.t.Helper()
	ran := make(chan struct{})
	r.app.Update(func() { defer close(ran); fn() })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		r.t.Fatal("the loop did not run")
	}
}

// the only placement in imgs
func onlyPlacement(imgs map[uint32]tui.ImagePlacement) (pl tui.ImagePlacement) {
	for _, pl = range imgs {
	}
	return pl
}

// An image-only paragraph in the Rendered view, on a terminal that draws kitty's graphics, is its
// picture: its rows reserved, blank, between the text before and after it, its image placed over
// them at its own size in cells (80 × 32 pixels, cells 8 × 16: 10 × 2), never its alt text.
func TestAnImageParagraphIsDrawnOverItsRows(t *testing.T) {
	r := runImageEditor(t, "before\n\n![gull]("+pngURI(t, 80, 32)+")\n\nafter", 40, 12, kitty)
	r.waitFor("the picture placed", func(_ string, imgs map[uint32]tui.ImagePlacement) bool { return len(imgs) == 1 })
	sc := r.tb.String()
	pl := onlyPlacement(r.tb.Images())
	if pl.Cols != 10 || pl.Rows != 2 || pl.X != 0 || len(pl.PNG) == 0 {
		t.Fatalf("placed %+v; want 10 × 2 at the left", pl)
	}
	before, after := lineOf(sc, "before"), lineOf(sc, "after")
	if strings.Contains(sc, "gull") || before < 0 || after < 0 || !(before < pl.Y && pl.Y+pl.Rows <= after) {
		t.Fatalf("the picture at rows %d-%d, the text at %d and %d:\n%s", pl.Y, pl.Y+pl.Rows-1, before, after, sc)
	}
	for _, l := range strings.Split(sc, "\n")[pl.Y : pl.Y+pl.Rows] {
		if strings.TrimSpace(l) != "" {
			t.Fatalf("a reserved row shows %q:\n%s", l, sc)
		}
	}
}

// Where the terminal can't draw images, the paragraph is its text, and no image is placed.
func TestAnImageParagraphIsItsTextWhereItCantBeDrawn(t *testing.T) {
	r := runImageEditor(t, "before\n\n![gull]("+pngURI(t, 80, 32)+")\n\nafter", 40, 12,
		tui.Capabilities{ColorProfile: tui.ProfileTrueColor})
	r.waitFor("the text after", func(sc string, _ map[uint32]tui.ImagePlacement) bool { return strings.Contains(sc, "after") })
	time.Sleep(50 * time.Millisecond)
	r.onLoop(func() {})
	if sc, imgs := r.tb.String(), r.tb.Images(); len(imgs) != 0 || !strings.Contains(sc, "gull") {
		t.Fatalf("no kitty graphics: images %+v, want none and the alt text:\n%s", imgs, sc)
	}
}

// An image is placed at its cells in the Editor: after the line numbers' gutter. A view switch (to
// Raw) takes it off: the App removes an image no longer reported.
func TestAnImageIsPlacedInTheEditorAndGoesWithItsView(t *testing.T) {
	r := runImageEditor(t, "before\n\n![gull]("+pngURI(t, 80, 32)+")\n\nafter", 40, 12, kitty, WithEditorLineNumbers(true))
	r.waitFor("the picture placed", func(_ string, imgs map[uint32]tui.ImagePlacement) bool { return len(imgs) == 1 })
	var gutter int
	r.onLoop(func() { gutter = r.e.cells.gutter })
	if pl := onlyPlacement(r.tb.Images()); gutter == 0 || pl.X != gutter {
		t.Errorf("placed at x %d, want after the gutter (%d)", pl.X, gutter)
	}
	r.onLoop(func() { r.e.SetView(ViewRaw) })
	r.waitFor("the picture taken off in Raw", func(_ string, imgs map[uint32]tui.ImagePlacement) bool { return len(imgs) == 0 })
}

// An image scrolled partly above the view is placed from its rows above the view on, which the App
// cuts to the rows that show: 80 × 128 pixels is 10 × 8 cells; scrolled three rows into it, the
// five rows of it on the view show the same part of the image, over blank rows, the text after it
// below.
func TestAnImageScrolledPartlyOffIsCropped(t *testing.T) {
	r := runImageEditor(t, "before\n\n![tall]("+pngURI(t, 80, 128)+")\n\nafter"+strings.Repeat("\n\nmore", 4), 40, 8, kitty)
	var first int
	r.waitFor("the picture placed", func(_ string, imgs map[uint32]tui.ImagePlacement) bool {
		if len(imgs) != 1 {
			return false
		}
		first = onlyPlacement(imgs).Y
		return true
	})
	r.onLoop(func() { r.e.cells.ScrollLines(first + 3) })
	r.waitFor("the picture cut", func(_ string, imgs map[uint32]tui.ImagePlacement) bool {
		pl := onlyPlacement(imgs)
		return len(imgs) == 1 && pl.Y == 0 && pl.Rows == 5 && pl.Cols == 10 && pl.Clip == tui.Rect{X: 0, Y: 48, W: 80, H: 80}
	})
	pl := onlyPlacement(r.tb.Images())
	for y, l := range strings.Split(r.tb.String(), "\n")[:pl.Rows] { // the picture's own cells: the scroll indicator's column is not its
		if cells := []rune(l)[pl.X : pl.X+pl.Cols]; strings.TrimSpace(string(cells)) != "" {
			t.Fatalf("row %d of the picture shows %q:\n%s", y, string(cells), r.tb.String())
		}
	}
	if s := r.tb.String(); strings.Contains(s, "before") || lineOf(s, "after") < 5 {
		t.Fatalf("scrolled into the picture, want its rows on top and the text after it below:\n%s", s)
	}
}

// heldImages is a resolver of each note's images: p.png the note's own picture, at once; s.png held
// for note A until released (a load still running when the note is switched), at once for B.
type heldImages struct {
	note    atomic.Value  // "A" or "B": the note shown, read when a load starts
	started chan struct{} // closed when note A's s.png load is in the resolver
	once    sync.Once
	release chan struct{}
	pngs    map[string]string // note + src → the PNG's data: URI
}

func (h *heldImages) resolve(ctx context.Context, src string) (io.ReadCloser, error) {
	note := h.note.Load().(string)
	if note == "A" && src == "s.png" {
		h.once.Do(func() { close(h.started) })
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	uri, ok := h.pngs[note+src]
	if !ok {
		return nil, errors.New("no such image")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:image/png;base64,"))
	return io.NopCloser(bytes.NewReader(b)), err
}

// An image is its buffer's: another note shown (SetBuffer) lays its own images, the same src read
// again (another note's p.png is another file), and a load of the first note's still running when
// it was switched, answered late, places nothing: only the shown note's images are placed.
func TestAnImageIsItsBuffersOwn(t *testing.T) {
	h := &heldImages{started: make(chan struct{}), release: make(chan struct{}), pngs: map[string]string{
		"Ap.png": pngURI(t, 80, 32), // 10 × 2
		"As.png": pngURI(t, 80, 64), // 10 × 4
		"Bp.png": pngURI(t, 40, 48), // 5 × 3
		"Bs.png": pngURI(t, 16, 16), // 2 × 1
	}}
	h.note.Store("A")
	note := func(n string) string { return "![p](p.png)\n\n![s](s.png)\n\nnote " + n }
	r := runImageEditor(t, note("A"), 40, 16, kitty)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(h.release) }) }) // before the App stops: a held load let go
	r.onLoop(func() { r.e.SetImages(h.resolve) })
	r.waitFor("note A's picture", func(sc string, imgs map[uint32]tui.ImagePlacement) bool {
		pl := onlyPlacement(imgs)
		return len(imgs) == 1 && pl.Cols == 10 && pl.Rows == 2 && strings.Contains(sc, "note A")
	})
	select { // note A's s.png is loading: its load runs on across the switch
	case <-h.started:
	case <-time.After(5 * time.Second):
		t.Fatal("note A's s.png never began to load")
	}
	stale := make(chan struct{})
	r.onLoop(func() {
		var id tui.TaskID
		for tid, src := range r.e.imgs.Tasks() {
			if src == "s.png" {
				id = tid
			}
		}
		if id == 0 {
			t.Error("no load of note A's s.png in flight")
		}
		r.e.taskDone = func(res tui.TaskResult) {
			if res.ID == id {
				close(stale)
			}
		}
		h.note.Store("B")
		r.e.Core().SetBuffer(NewBuffer(BufferText(note("B"))))
	})
	only := func(sc string, imgs map[uint32]tui.ImagePlacement) bool {
		var p, s bool
		for _, pl := range imgs {
			p = p || pl.Cols == 5 && pl.Rows == 3
			s = s || pl.Cols == 2 && pl.Rows == 1
		}
		return len(imgs) == 2 && p && s && strings.Contains(sc, "note B") && !strings.Contains(sc, "note A")
	}
	r.waitFor("note B's own pictures", only)
	once.Do(func() { close(h.release) }) // note A's s.png answers now
	select {                             // its result taken by the Editor
	case <-stale:
	case <-time.After(5 * time.Second):
		t.Fatal("note A's late load never came back")
	}
	flushes := r.tb.Flushes()
	r.onLoop(func() { r.e.MarkDirty() }) // a frame painted after the late result was handled
	r.waitFor("a frame after the late load", func(string, map[uint32]tui.ImagePlacement) bool { return r.tb.Flushes() > flushes })
	if sc, imgs := r.tb.String(), r.tb.Images(); !only(sc, imgs) {
		t.Fatalf("after note A's late load: images %+v, want only note B's (5 × 3 and 2 × 1):\n%s", imgs, sc)
	}
}
