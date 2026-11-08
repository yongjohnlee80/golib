package widget

import (
	"context"
	"strings"
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
