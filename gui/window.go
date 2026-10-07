package gui

import (
	"image"
	"io"
	"strings"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// textType is the MIME type the clipboard is read and written as.
const textType = "application/text"

// gioState is what the Gio goroutine owns.
type gioState struct {
	shaper  *text.Shaper // the preedit's; the cells have their own on tui's loop
	ops     op.Ops
	ime     imeState
	ptr     pointerState
	caret   image.Rectangle // the caret the IME was last told about
	focused bool            // key focus requested for the window's tag
	shown   *frame          // the frame last submitted
}

func (c config) windowOptions() []app.Option {
	opts := []app.Option{app.Title(c.title), app.Size(c.width, c.height)}
	if c.minWidth > 0 && c.minHeight > 0 {
		opts = append(opts, app.MinSize(c.minWidth, c.minHeight))
	}
	if c.decoration {
		opts = append(opts, app.Decorated(c.decorated))
	}
	return opts
}

func fontOf(typeface string) font.Font { return font.Font{Typeface: font.Typeface(typeface)} }

// run is the Gio goroutine. It never touches a component or the grid: it turns the window's
// events into tui events through the queue, publishes metrics, and submits the newest frame.
func (b *Backend) run() {
	tag := &b.gio // the window's one input handler
	for {
		switch e := b.win.Event().(type) {
		case app.DestroyEvent:
			b.q.end(e.Err) // a no-op when Stop ended it first: the first reason is kept
			return

		case app.ConfigEvent:
			if was := b.focused.Swap(e.Config.Focused); was != e.Config.Focused && b.isStarted() {
				b.q.push(tui.FocusEvent{Terminal: true, Gained: e.Config.Focused})
			}

		case app.FrameEvent:
			m := measure(b.fm, b.cfg.fontSize, b.cfg.padding, e.Size, e.Metric)
			if old := b.metrics.Swap(&m); old == nil {
				b.startOnce.Do(func() { close(b.started) }) // Start returns; nothing is sent
			} else if !old.sameCells(m) {
				// The grid, the scale or the cell size changed: the App relays out and repaints.
				b.q.push(tui.ResizeEvent{W: m.grid.W, H: m.grid.H})
			}
			b.readInput(e.Source, tag, m)
			b.present(e, tag, m)
		}
	}
}

func (b *Backend) isStarted() bool {
	select {
	case <-b.started:
		return true
	default:
		return false
	}
}

// readInput reads this frame's events: keys, text, pointer, clipboard.
func (b *Backend) readInput(src eventSource, tag event.Tag, m metrics) {
	if !b.gio.focused {
		src.Execute(key.FocusCmd{Tag: tag})
		b.gio.focused = true
	}
	allMods := key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt | key.ModSuper
	for {
		ev, ok := src.Event(
			key.FocusFilter{Target: tag},
			key.Filter{Focus: tag, Optional: allMods},
			pointer.Filter{
				Target:  tag,
				Kinds:   pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Scroll | pointer.Cancel,
				ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
				ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
			},
			transfer.TargetFilter{Target: tag, Type: textType},
		)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.Event:
			if isPasteChord(e) {
				src.Execute(clipboard.ReadCmd{Tag: tag})
				continue
			}
			if k, ok := translateKey(e); ok {
				b.q.push(k)
			}
		case key.EditEvent:
			b.gio.ime.edit(e)
		case key.CompositionEvent:
			b.gio.ime.compose(e)
		case key.FocusEvent:
			b.gio.focused = e.Focus
		case pointer.Event:
			for _, me := range b.gio.ptr.translate(e, m) {
				b.q.push(me)
			}
		case transfer.DataEvent:
			if r := e.Open(); r != nil {
				data, _ := io.ReadAll(r)
				r.Close()
				text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
				b.q.push(tui.PasteEvent{Text: text})
			}
		}
	}
	if s, ok := b.gio.ime.take(); ok {
		for _, k := range textKeys(s) {
			b.q.push(k)
		}
		// Empty Gio's copy of the input method's buffer too, so the next edit starts at 0.
		src.Execute(key.SnippetCmd{Tag: tag})
		src.Execute(key.SelectionCmd{Tag: tag})
	}
	b.mu.Lock()
	cp := b.copyText
	b.copyText = nil
	b.mu.Unlock()
	if cp != nil {
		src.Execute(clipboard.WriteCmd{Type: textType, Data: io.NopCloser(strings.NewReader(*cp))})
	}
}

// eventSource is the part of Gio's input.Source the backend uses, so tests can drive it.
type eventSource interface {
	Event(filters ...event.Filter) (event.Event, bool)
	Execute(c input.Command)
}

// present submits the newest frame. Before the App's first Flush there is none, and the window
// is submitted with its background alone: Gio needs valid ops for every FrameEvent.
func (b *Backend) present(e app.FrameEvent, tag event.Tag, m metrics) {
	b.mu.Lock()
	if b.latest != nil {
		b.gio.shown, b.latest = b.latest, nil
	}
	f := b.gio.shown
	b.mu.Unlock()

	ops := &b.gio.ops
	ops.Reset()
	fillRect(ops, image.Rectangle{Max: e.Size}, b.cfg.theme.BG)
	if f != nil {
		f.draw.Add(ops)
		b.placeCaret(e.Source, tag, f)
		b.drawPreedit(ops, f, m)
	}
	// The window's one input area: keys, pointer, clipboard all address tag.
	area := clip.Rect(image.Rectangle{Max: e.Size}).Push(ops)
	event.Op(ops, tag)
	area.Pop()
	e.Frame(ops)
}

// placeCaret tells the input method where the caret is, so its candidate window opens there.
func (b *Backend) placeCaret(src eventSource, tag event.Tag, f *frame) {
	if f.caret == b.gio.caret || f.caret.Empty() {
		return
	}
	b.gio.caret = f.caret
	n := len(b.gio.ime.text)
	src.Execute(key.SelectionCmd{
		Tag:   tag,
		Range: key.Range{Start: n, End: n},
		Caret: key.Caret{
			Pos:     f32.Pt(float32(f.caret.Min.X), float32(f.caret.Min.Y+f.base)),
			Ascent:  float32(f.base),
			Descent: float32(f.caret.Dy() - f.base),
		},
	})
}

// drawPreedit draws the text being composed at the caret, underlined, as terminals do. It is not
// in the grid: the App sees it only once committed.
func (b *Backend) drawPreedit(ops *op.Ops, f *frame, m metrics) {
	pre := b.gio.ime.preedit()
	if pre == "" || f.caret.Empty() {
		return
	}
	s := b.gio.shaper
	s.LayoutString(text.Parameters{Font: fontOf(fallbackChain(b.cfg.typeface)), PxPerEm: fixed.Int26_6(m.ppem * 64), MaxWidth: 1 << 20}, pre)
	var glyphs []text.Glyph
	var adv fixed.Int26_6
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		glyphs = append(glyphs, g)
		adv += g.Advance
	}
	if len(glyphs) == 0 {
		return
	}
	box := image.Rect(f.caret.Min.X, f.caret.Min.Y, f.caret.Min.X+adv.Ceil(), f.caret.Max.Y)
	fillRect(ops, box, b.cfg.theme.BG)
	off := op.Offset(image.Pt(box.Min.X, box.Min.Y+f.base)).Push(ops)
	outline := clip.Outline{Path: s.Shape(glyphs)}.Op().Push(ops)
	paint.ColorOp{Color: b.cfg.theme.FG}.Add(ops)
	paint.PaintOp{}.Add(ops)
	outline.Pop()
	s.Bitmaps(glyphs).Add(ops)
	off.Pop()
	lw := max(1, int(m.scale+0.5))
	fillRect(ops, image.Rect(box.Min.X, box.Max.Y-lw, box.Max.X, box.Max.Y), b.cfg.theme.FG)
}
