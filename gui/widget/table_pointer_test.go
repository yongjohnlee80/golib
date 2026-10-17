package widget

import (
	"context"
	"fmt"
	"image/color"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// tablePixels is a test backend whose cells are 8 by 16 pixels, as a native window's are: a
// table's pointer reaches its layout in pixels.
type tablePixels struct{ *tui.TestBackend }

func (tablePixels) CellPixels() tui.CellPixels { return tui.CellPixels{W: 8, H: 16} }

// tableApp is a pixel table running in an App, beside a button the focus can move to. Its layout
// is painted on the loop, 600 by 200, as the window paints it.
type tableApp struct {
	t     *testing.T
	app   *tui.App
	tb    *tui.TestBackend
	dt    *tuiwidget.DataTable
	l     *tableLayout
	other *tuiwidget.Button
}

func startTableApp(t *testing.T, src tuiwidget.TableSource, opts ...tuiwidget.TableOption) *tableApp {
	t.Helper()
	dt, l := pixelTable(t, src, opts...)
	other := tuiwidget.NewButton("other")
	root := tui.NewStack()
	root.AddAt(dt, 0, 0)
	root.AddAt(other, 0, 19)
	tb := tui.NewTestBackend(80, 20)
	app := tui.NewApp(root, tui.WithBackend(tablePixels{tb}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the App did not stop")
		}
	})
	waitFor(t, "a frame", func() bool { return tb.Flushes() > 0 })
	h := &tableApp{t: t, app: app, tb: tb, dt: dt, l: l, other: other}
	h.onLoop(func() {
		app.FocusInto(dt)
		paintTable(l, 600, 200)
	})
	return h
}

func (h *tableApp) onLoop(fn func()) {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		h.t.Fatal("the loop did not run the update")
	}
}

// until polls cond on the loop until it holds; on a timeout it fails with what state reports.
func (h *tableApp) until(what string, cond func() bool, state func() string) {
	h.t.Helper()
	for range 500 {
		var ok bool
		h.onLoop(func() { ok = cond() })
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	var s string
	h.onLoop(func() { s = state() })
	h.t.Fatalf("timed out waiting for %s: %s", what, s)
}

// pointer is a left-button event at pixel (x, y) of the table, as the window sends it: the cell
// under the point and where inside it. A motion with the button held is MouseMotion with
// MouseLeft, as the window's pointer translation reports a drag.
func pointer(kind tui.MouseKind, x, y float32) tui.MouseEvent {
	cx, cy := int(x/8), int(y/16)
	return tui.MouseEvent{Kind: kind, Button: tui.MouseLeft, X: cx, Y: cy, SubX: x/8 - float32(cx), SubY: y/16 - float32(cy)}
}

// drag presses at the first point and drags through the rest, holding the button: the table
// keeps the pointer (Context.CapturePointer) while it drags. release lets it go.
func (h *tableApp) drag(pts ...[2]float32) {
	h.t.Helper()
	evs := []tui.Event{pointer(tui.MousePress, pts[0][0], pts[0][1])}
	for _, p := range pts[1:] {
		evs = append(evs, pointer(tui.MouseMotion, p[0], p[1]))
	}
	if err := h.tb.Inject(evs...); err != nil {
		h.t.Fatal(err)
	}
}

// move drags on to (x, y), the button still held.
func (h *tableApp) move(x, y float32) {
	h.t.Helper()
	if err := h.tb.Inject(pointer(tui.MouseMotion, x, y)); err != nil {
		h.t.Fatal(err)
	}
}

func (h *tableApp) release(x, y float32) {
	h.t.Helper()
	if err := h.tb.Inject(pointer(tui.MouseRelease, x, y)); err != nil {
		h.t.Fatal(err)
	}
	h.until("the release", func() bool { return !h.dt.Core().Dragging() && !h.dt.Context().HasPointerCapture() }, h.state)
}

func (h *tableApp) state() string {
	c := h.dt.Core()
	from, to, ok := c.Selection()
	return fmt.Sprintf("kind %v, selection %v %+v to %+v, cursor %+v, dragging %v, capture %v, focused %v, copy %q",
		c.SelectionKind(), ok, from, to, c.Cursor(), c.Dragging(), h.dt.Context().HasPointerCapture(), h.dt.Context().Focused(), c.CopyText())
}

// The pointer through DataTable.HandleEvent with the pixel layout: a press on a field's title
// selects that field (not field 0), every row, and a drag across the titles widens it; a drag over
// the gutter selects whole rows; a drag over cells selects them in reading order. The table holds
// the pointer from the press to the release.
func TestATableSelectsByThePointer(t *testing.T) {
	h := startTableApp(t, cities(30), tuiwidget.TableRowNumbers(true))
	var cols []tableColumn
	var g, rowH float32
	h.onLoop(func() { cols, g, rowH = h.l.columns(600), h.l.gutter(), h.l.rowH })
	mid := func(c tableColumn) float32 { return c.x + c.w/2 }
	band := func(row int) float32 { return (float32(row) + 1.5) * rowH }

	h.drag([2]float32{mid(cols[2]), rowH / 2})
	h.until("a press on field 2's title to select it", func() bool {
		from, to, ok := h.dt.Core().Selection()
		return ok && h.dt.Core().SelectionKind() == tuiwidget.TableSelectBlock &&
			from == (tuiwidget.TablePos{Row: 0, Col: 2}) && to == (tuiwidget.TablePos{Row: 29, Col: 2}) &&
			h.dt.Context().HasPointerCapture()
	}, h.state)
	h.move(mid(cols[3]), rowH/2)
	h.until("a drag across the titles to widen the field", func() bool {
		from, to, _ := h.dt.Core().Selection()
		return from == (tuiwidget.TablePos{Row: 0, Col: 2}) && to == (tuiwidget.TablePos{Row: 29, Col: 3})
	}, h.state)
	h.release(mid(cols[3]), rowH/2)

	h.drag([2]float32{g / 2, band(1)}, [2]float32{g / 2, band(2)}, [2]float32{g / 2, band(3)})
	h.until("a drag over the gutter to select rows 1-3", func() bool {
		return h.dt.Core().SelectionKind() == tuiwidget.TableSelectRows && h.dt.Context().HasPointerCapture() &&
			h.dt.Core().CopyText() == "1\trow1\tSeoul\t10\n2\trow2\tSeoul\t20\n3\trow3\tSeoul\t30"
	}, h.state)
	h.release(g/2, band(3))
	var fills []gui.Rect
	h.onLoop(func() { fills = fillsIn(paintTable(h.l, 600, 200), h.l.selection) })
	whole := gui.Rect{X: cols[0].x - h.l.pad, Y: 2 * rowH, W: cols[3].x + cols[3].w - cols[0].x + 2*h.l.pad, H: 3 * rowH}
	if len(fills) != 1 || fills[0] != whole {
		t.Errorf("rows 1-3 selected by the gutter filled %+v, want their whole rows %+v", fills, whole)
	}

	h.drag([2]float32{mid(cols[1]), band(1)}, [2]float32{mid(cols[2]), band(2)}, [2]float32{mid(cols[2]), band(3)})
	h.until("a drag over cells to select row 1 field 1 to row 3 field 2", func() bool {
		return h.dt.Core().SelectionKind() == tuiwidget.TableSelectCells &&
			h.dt.Core().CopyText() == "row1\tSeoul\t10\n2\trow2\tSeoul\t20\n3\trow3\tSeoul"
	}, h.state)
	h.release(mid(cols[2]), band(3))
}

// The cursor's cell is filled in the DataTable's CursorStyle: Cursor while the table has the
// keys, CursorBlurred once the focus moves to the button, Cursor again when it comes back; the
// focus change paints a frame. With the styles' grounds given, as decl's palette gives them, those
// are the colours.
func TestATableBlursItsCursorWithTheFocus(t *testing.T) {
	h := startTableApp(t, cities(30))
	cursorFill := func() (got []color.NRGBA) {
		h.onLoop(func() {
			c := paintTable(h.l, 600, 200)
			cur := h.dt.Core().Cursor()
			want, _ := h.l.cellsRect(h.l.columns(600), cur, cur, h.l.top+h.l.bodyRows())
			for _, f := range calls(c, "FillRect") {
				if f.Rect == want {
					got = append(got, f.Brush.Color)
				}
			}
		})
		return got
	}
	focus := func(to tui.Component, table bool) {
		t.Helper()
		before := h.tb.Flushes()
		h.onLoop(func() { h.app.FocusInto(to) })
		h.until("the focus", func() bool { return h.dt.Context().Focused() == table }, h.state)
		waitFor(t, "a frame for the focus change", func() bool { return h.tb.Flushes() > before })
	}
	expect := func(when string, want color.NRGBA) {
		t.Helper()
		if got := cursorFill(); len(got) != 1 || got[0] != want {
			t.Errorf("%s: the cursor's cell filled %v, want %v", when, got, want)
		}
		var textColor color.NRGBA
		var textSeen bool
		h.onLoop(func() {
			c := paintTable(h.l, 600, 200)
			cur := h.dt.Core().Cursor()
			rect, _ := h.l.cellsRect(h.l.columns(600), cur, cur, h.l.top+h.l.bodyRows())
			for _, call := range calls(c, "DrawText") {
				if call.Rect.Y >= rect.Y && call.Rect.Y < rect.Y+rect.H {
					textColor, textSeen = call.Brush.Color, true // the last is the clipped overlay
				}
			}
		})
		if !textSeen {
			t.Errorf("%s: the cursor has no text overlay", when)
		} else if ratio, ok := style.ContrastRatio(style.RGB(textColor.R, textColor.G, textColor.B), style.RGB(want.R, want.G, want.B)); !ok || ratio < 4.5 {
			t.Errorf("%s: the cursor text %v on %v has contrast %.2f", when, textColor, want, ratio)
		}
	}
	var cursor, blurred color.NRGBA
	h.onLoop(func() { cursor, blurred = h.l.cursor, h.l.blurred })
	if cursor == blurred {
		t.Fatalf("the cursor and the blurred cursor are both %v", cursor)
	}
	expect("with the keys", cursor)
	focus(h.other, false)
	expect("after the focus moved to the button", blurred)
	focus(h.dt, true)
	expect("with the focus back", cursor)

	red, blue := color.NRGBA{R: 200, A: 255}, color.NRGBA{B: 200, A: 255}
	h.onLoop(func() {
		h.dt.SetStyles(tuiwidget.DataTableStyles{
			Cursor:        style.New().Background(style.RGB(200, 0, 0)).Foreground(style.RGB(255, 255, 255)),
			CursorBlurred: style.New().Background(style.RGB(0, 0, 200)),
		})
	})
	expect("styled, with the keys", red)
	focus(h.other, false)
	expect("styled, after the focus moved", blue)
	focus(h.dt, true)
	expect("styled, with the focus back", red)
}
