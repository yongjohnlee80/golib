package gui

import (
	"image"
	"image/color"
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
			fs := b.font.Load()
			m := measure(fs.fm, fs.size, b.cfg.padding, e.Size, e.Metric)
			m.fontGen = fs.gen
			if old := b.metrics.Swap(&m); old == nil {
				b.startOnce.Do(func() { close(b.started) }) // Start returns; nothing is sent
			} else if !old.sameCells(m) {
				// The grid, the scale or the cell size changed: the App relays out and repaints.
				b.q.push(tui.ResizeEvent{W: m.grid.W, H: m.grid.H})
			}
			b.readInput(e.Source, tag, m)
			b.present(e, tag, m, fs)
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
		// Gio closes text input on every focus change, and an input method attaches only to a
		// window that opened it: on Wayland, zwp_text_input_v3 is enabled only then, so
		// without this fcitx5 or IBus never composes Hangul here.
		src.Execute(key.SoftKeyboardCmd{Show: true})
		b.gio.focused = true
	}
	allMods := key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt | key.ModSuper
	for {
		ev, ok := src.Event(
			key.FocusFilter{Target: tag},
			key.Filter{Focus: tag, Optional: allMods},
			// The window sends Tab and Shift+Tab as system events, for Gio's own focus
			// traversal, and the catch-all filter above never matches one: name them.
			key.Filter{Focus: tag, Name: key.NameTab, Optional: key.ModShift},
			pointer.Filter{
				Target:  tag,
				Kinds:   pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Scroll | pointer.Cancel | pointer.Leave,
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
			if e.Kind == pointer.Leave {
				b.q.push(tui.PointerLeaveEvent{}) // nothing in the window is under the pointer
				continue
			}
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
	if pieces, ok := b.gio.ime.take(); ok {
		for _, s := range pieces {
			for _, k := range textKeys(s) {
				b.q.push(k)
			}
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
func (b *Backend) present(e app.FrameEvent, tag event.Tag, m metrics, fs *fontState) {
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
		b.drawPreedit(ops, f, m, fs)
	}
	// The window's one input area: keys, pointer, clipboard all address tag.
	area := clip.Rect(image.Rectangle{Max: e.Size}).Push(ops)
	event.Op(ops, tag)
	area.Pop()
	e.Frame(ops)
}

// placeCaret tells the input method where the caret is, so its candidate window opens there.
//
// The selection is told on every frame the frame has a caret, not only when the caret moves:
// macOS's input method inserts a typed character at the selection it was last told
// (insertText:replacementRange: with no range), so a stale selection inserts at an old
// position — a space typed right after a same-width one arrived only with the next key.
// The caret geometry goes with it every time: Gio stores the command's caret verbatim,
// so a zero caret on an unmoved frame would move the candidate window to the origin.
func (b *Backend) placeCaret(src eventSource, tag event.Tag, f *frame) {
	if f.caret.Empty() {
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

// drawPreedit draws what the input method holds at the caret: committed text that waits for the
// composition to end, then the composition itself, underlined as terminals do. It is not in the
// grid: the App sees it only once delivered.
func (b *Backend) drawPreedit(ops *op.Ops, f *frame, m metrics, fs *fontState) {
	held, comp, ok := b.gio.ime.held()
	if !ok || f.caret.Empty() {
		return
	}
	s := b.gio.shaper
	s.LayoutString(text.Parameters{Font: fontOf(fs.typeface), PxPerEm: fixed.Int26_6(m.ppem * 64), MaxWidth: 1 << 20}, string(held))
	var glyphs []text.Glyph
	var adv fixed.Int26_6
	// the composition's span among the held text, at cluster starts: only it is underlined
	runes, ulFrom, ulTo := 0, fixed.Int26_6(-1), fixed.Int26_6(-1)
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		if g.Runes > 0 {
			if ulFrom < 0 && runes >= comp.Start {
				ulFrom = adv
			}
			if ulTo < 0 && runes >= comp.End {
				ulTo = adv
			}
			runes += int(g.Runes)
		}
		glyphs = append(glyphs, g)
		adv += g.Advance
	}
	if len(glyphs) == 0 {
		return
	}
	if ulFrom < 0 {
		ulFrom = adv
	}
	if ulTo < 0 {
		ulTo = adv
	}
	box := image.Rect(f.caret.Min.X, f.caret.Min.Y, f.caret.Min.X+adv.Ceil(), f.caret.Max.Y)
	fg, bg := preeditColors(f, b.cfg.theme.FG, b.cfg.theme.BG)
	fillRect(ops, box, bg)
	off := op.Offset(image.Pt(box.Min.X, box.Min.Y+f.base)).Push(ops)
	outline := clip.Outline{Path: s.Shape(glyphs)}.Op().Push(ops)
	paint.ColorOp{Color: fg}.Add(ops)
	paint.PaintOp{}.Add(ops)
	outline.Pop()
	s.Bitmaps(glyphs).Add(ops)
	off.Pop()
	lw := max(1, int(m.scale+0.5))
	fillRect(ops, image.Rect(box.Min.X+ulFrom.Round(), box.Max.Y-lw, box.Min.X+ulTo.Round(), box.Max.Y), fg)
}

// preeditColors are the text being composed's colours: the text's at the caret, so it reads as
// part of the line; the window theme's (themeFG, themeBG) when they are unknown or the text has
// no opaque background of its own.
func preeditColors(f *frame, themeFG, themeBG color.NRGBA) (fg, bg color.NRGBA) {
	if !f.tinted || f.bg.A == 0 {
		return themeFG, themeBG
	}
	fg = f.fg
	if fg.A == 0 {
		fg = themeFG
	}
	return fg, f.bg
}
