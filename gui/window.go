package gui

import (
	"image"
	"image/color"
	"io"
	"strings"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/gio/font"
	"github.com/yongjohnlee80/gio/io/clipboard"
	"github.com/yongjohnlee80/gio/io/event"
	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/io/key"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/io/transfer"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/op/paint"
	"github.com/yongjohnlee80/gio/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// textType is the MIME type the clipboard is read and written as.
const textType = "application/text"

// Gio Window Event Loop & GPU Presentation Architecture:
//
//	              +------------------------+
//	              |   Gio app.Window       |
//	              +------------------------+
//	                          |
//	        +-----------------+-----------------+
//	        |                 |                 |
//	        v                 v                 v
//	  ConfigEvent        FrameEvent       DestroyEvent
//	  (Focus shift)     (Rasterize ops)   (Exit/Cleanup)
//	        |                 |                 |
//	        |       +---------+---------+       v
//	        |       |                   |    b.q.end()
//	        |       v                   v
//	        |   readInput()         present()
//	        |   (Keys/Mouse/IME)    (Latest CallOp + Caret)
//	        v       |                   |
//	  +-------------+                   v
//	  | Push Events |               e.Frame(ops)
//	  v             v               (GPU Output)
//	eventQueue -> Backend.Events()
//
// gioState encapsulates state owned exclusively by the Gio window goroutine.
type gioState struct {
	shaper  *text.Shaper          // Preedit IME text shaper (independent of TUI loop text shaper)
	ops     op.Ops                // Reusable Gio GPU operation buffer
	ime     imeState              // Virtual IME input buffer and composition tracking
	rawOn   bool                  // raw keys are applied (rawkeys.go)
	held    map[rune]tui.KeyEvent // raw keys reported pressed and not yet released
	ptr     pointerState          // Pointer position, button transitions, and drag tracker
	caret   image.Rectangle       // Physical pixel bounding box of the active caret
	focused bool                  // Tracks whether OS keyboard focus has been registered for this window
	frames  int                   // Frames whose input readInput has read, for the recorder
	shown   *frame                // Most recently presented frame macro
	source  input.Source          // Last frame's router, for committing IME before an external paste
}

// takeFocus is the handshake readInput runs while the window has not taken the
// keyboard focus: taking it and opening the text input, which is what lets an
// input method attach at all. Gio closes text input on every focus change, and
// on Wayland zwp_text_input_v3 is enabled only for a window that opened it, so
// without this fcitx5 or IBus never composes Hangul here — and the handshake
// repeats whenever the focus was lost. The window sends the commands; the replay
// checks them.
func (g *gioState) takeFocus() []TextInputCmd {
	if g.focused {
		return nil
	}
	g.focused = true
	if g.rawOn {
		return []TextInputCmd{FocusTake{}, KeyboardHide{}} // raw keys: the input method stays off
	}
	return []TextInputCmd{FocusTake{}, KeyboardShow{}}
}

// windowOptions builds the slice of Gio app.Option values from the configuration struct.
func (c config) windowOptions() []app.Option {
	opts := []app.Option{app.Title(c.title), app.Size(c.width, c.height)}
	if c.minWidth > 0 && c.minHeight > 0 {
		opts = append(opts, app.MinSize(c.minWidth, c.minHeight))
	}
	if c.decoration {
		opts = append(opts, app.Decorated(c.decorated))
	}
	if c.zeroCopy {
		opts = append(opts, app.PreferVulkan())
	}
	return opts
}

// fontOf constructs a Gio font.Font descriptor for the given typeface name.
func fontOf(typeface string) font.Font { return font.Font{Typeface: font.Typeface(typeface)} }

// run executes the primary Gio event loop on a dedicated goroutine spawned by [Backend.Start].
//
// Invariants & Responsibilities:
//   - Isolated from the TUI event loop: run never inspects or mutates component trees, widget states,
//     or cell grid arrays directly.
//   - Reads incoming [app.Window] events (keyboard, mouse, window resizing, focus shifts, close requests).
//   - Translates raw window events into standard [tui.Event] types and pushes them onto the [eventQueue].
//   - Measures window metrics and publishes atomic snapshots via [Backend.metrics].
//   - Renders the latest frame macro submitted by [Backend.Flush] into the GPU display context.
func (b *Backend) run() {
	defer b.finishRun() // the recorder is closed here, by its only writer, before Stop returns
	tag := &b.gio       // the window's one input handler
	for {
		switch e := b.win.Event().(type) {
		case app.DropEvent:
			b.desktopDrop(e)
		case *app.ClosingEvent:
			if b.askToClose() {
				e.Abort()
			}

		case app.DestroyEvent:
			b.q.end(e.Err) // a no-op when Stop ended it first: the first reason is kept
			return

		case app.ConfigEvent:
			if was := b.focused.Swap(e.Config.Focused); was != e.Config.Focused && b.isStarted() {
				if !e.Config.Focused {
					b.releaseHeld() // the window lost focus: no key stays down
				}
				b.q.push(tui.FocusEvent{Terminal: true, Gained: e.Config.Focused})
			}

		case app.FrameEvent:
			b.gio.source = e.Source
			fs := b.font.Load()
			m := measure(fs.fm, fs.size, b.cfg.padding, e.Size, e.Metric)
			m.fontGen = fs.gen
			old := b.metrics.Load()
			if b.cfg.stage != nil {
				m = b.publishLayout(old, placePanes(b.cfg.stage, m, max(e.Metric.Dp(b.cfg.padding), 0)))
			}
			if old := b.metrics.Swap(&m); old == nil {
				b.startOnce.Do(func() { close(b.started) }) // Start returns; nothing is sent
			} else if !old.sameCells(m) {
				// The grid, the scale or the cell size changed: the App relays out and repaints.
				b.q.push(tui.ResizeEvent{W: m.grid.W, H: m.grid.H})
			}
			b.readInput(e.Source, tag, b.inputMetrics(m))
			b.present(e, tag, m, fs)
		}
	}
}

// isStarted reports whether the backend's window and initial metrics have finished initializing.
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
	b.gio.frames++
	if b.rec != nil {
		b.record(StageFrame, "begin", map[string]any{"n": b.gio.frames})
	}
	b.applyRawMode(src, tag)
	for _, cmd := range b.gio.takeFocus() {
		b.execRecorded(src, tag, cmd)
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
			if b.gio.rawOn {
				b.rawKey(e) // a game's key: no paste, no text
				continue
			}
			if isPasteChord(e) {
				src.Execute(clipboard.ReadCmd{Tag: tag})
				continue
			}
			if k, ok := translateKey(e); ok {
				// Committed text arrived before this key: deliver it first, or an Enter typed
				// after Hangul lands before the syllables the composition held.
				b.deliverIME(src, tag)
				b.q.push(k)
			}
		case key.SnippetEvent, key.SelectionEvent, key.EditEvent, key.CompositionEvent, key.FocusEvent:
			// The input-method events cross the platform contract; the window's other input
			// keeps its Gio types for now.
			ev, ok := inputEventOf(e)
			if !ok {
				continue
			}
			if fc, ok := ev.(FocusChange); ok && !fc.Focused {
				b.releaseHeld() // the handler lost the keyboard: no key stays down
			}
			if _, focus := ev.(FocusChange); b.gio.rawOn && !focus {
				// Raw keys: no input-method edits, even late ones. A focus change still runs as in
				// text mode, so a window that lost the keyboard asks for it back.
				continue
			}
			if b.rec != nil {
				kind, data := eventFields(ev)
				b.record(StageIn, kind, data)
			}
			switch ev := ev.(type) {
			case SnippetAsk:
				// Gio asks for the text around the input method's edits. Unanswered, its copy of
				// the editor state differs from the input method's and it cancels the composition.
				b.execRecorded(src, tag, b.gio.ime.snippet(ev))
			case SelectionSet:
				b.gio.ime.setSelection(ev)
			case TextEdit:
				b.gio.ime.edit(ev)
			case Composition:
				b.gio.ime.compose(ev)
			case FocusChange:
				b.gio.focused = ev.Focused
			}
			if b.rec != nil {
				b.record(StageIME, "imeState", imeFields(&b.gio.ime))
			}
		case pointer.Event:
			if e.Kind == pointer.Leave {
				b.q.push(tui.PointerLeaveEvent{}) // nothing in the window is under the pointer
				continue
			}
			for _, me := range b.gio.ptr.translate(e, m) {
				// A Panes window hands the App the device position and the frame on screen, and
				// the App resolves the cells when it dispatches the event (ResolvePointer). X and
				// Y stay this goroutine's mapping, for a host that does not resolve.
				if f := b.gio.shown; b.cfg.stage != nil && f != nil && f.m.layout != nil {
					me.Source = tui.PointerSource{X: e.Position.X, Y: e.Position.Y, Ref: f}
				}
				b.q.push(me)
			}
		case transfer.DataEvent:
			if data, ok := transferText(e); ok {
				b.q.push(tui.NewPasteEvent(data))
			}
		}
	}
	b.readClipboardReplies(src)
	b.deliverIME(src, tag)
	if b.rec != nil {
		b.record(StageFrame, "end", map[string]any{"n": b.gio.frames})
	}
	b.mu.Lock()
	cp := b.copyText
	b.copyText = nil
	b.mu.Unlock()
	if cp != nil {
		src.Execute(clipboard.WriteCmd{Type: textType, Data: io.NopCloser(strings.NewReader(*cp))})
	}
}

// deliverIME sends the text the input method has committed to the App, once no composition
// holds it, and empties the buffer and Gio's copy of it where the text ended, so Gio's
// selection does not move and nothing the input method is still composing is cancelled.
func (b *Backend) deliverIME(src eventSource, tag event.Tag) {
	keys, resets, ok := b.gio.ime.commitFrame()
	if !ok {
		return
	}
	for _, k := range keys {
		b.q.push(k)
	}
	if b.rec != nil {
		b.record(StageOut, "KeyEvent", map[string]any{"events": keys})
	}
	for _, cmd := range resets {
		b.execRecorded(src, tag, cmd)
	}
}

// execRecorded sends one TextInputCmd through the adapter, recording it first.
// Every TextInputCmd the backend emits goes through here — the prologue's focus
// handshake, the snippet answers, the delivery resets and the caret's selection —
// so a recording shows everything sent to the toolkit, not only what its next
// state implies.
func (b *Backend) execRecorded(src eventSource, tag event.Tag, cmd TextInputCmd) {
	if b.rec != nil {
		kind, data := cmdFields(cmd)
		b.record(StageCmd, kind, data)
	}
	execTextInputCmd(src, tag, cmd)
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
		countReplay(f.drawCost)
		for _, p := range f.parts {
			p.slot.add(ops) // the stream's newest picture, in paint order
			p.seg.Add(ops)
			countReplay(p.cost)
		}
		b.placeCaret(e.Source, tag, f)
		b.drawPreedit(ops, f, m, fs)
	}
	// The window's one input area: keys, pointer, clipboard all address tag.
	area := clip.Rect(image.Rectangle{Max: e.Size}).Push(ops)
	countClip()
	event.Op(ops, tag)
	area.Pop()
	e.Frame(ops)
	b.streamsPresented(e.Now)
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
	sel := b.gio.ime.selection() // the input method's own, so a composition is never cancelled
	b.execRecorded(src, tag, SelectionTell{Start: sel.Start, End: sel.End, Caret: caretPlace(f.caret, f.base)})
}

// caretPlace is a caret rectangle and its baseline (from the rectangle's top) as the input
// method takes them: the baseline's point, with the ascent above and the descent below it.
func caretPlace(caret image.Rectangle, base int) CaretPlace {
	return CaretPlace{
		X:       float32(caret.Min.X),
		Y:       float32(caret.Min.Y + base),
		Ascent:  float32(base),
		Descent: float32(caret.Dy() - base),
	}
}

// drawPreedit draws what the input method holds at the caret: committed text that waits for the
// composition to end, and the composition itself, in buffer order, all underlined as terminals
// underline a preedit: none of it is in the document yet. It is not in the grid: the App sees it
// only once delivered. The whole box is underlined, never a part mapped from rune offsets, so
// right-to-left text and zero-width marks need no visual-order bookkeeping.
func (b *Backend) drawPreedit(ops *op.Ops, f *frame, m metrics, fs *fontState) {
	held, comp, ok := b.gio.ime.held()
	if b.rec != nil && ok {
		// Frames with nothing held record nothing here: there is no preedit to draw, and
		// the caret's own record is the command placeCaret just sent. "drawn" carries the
		// anomaly: held text that an empty caret kept off the screen.
		b.record(StageDraw, "preedit", map[string]any{
			"held":  string(held),
			"comp":  map[string]any{"start": comp.Start, "end": comp.End},
			"caret": map[string]any{"x": f.caret.Min.X, "y": f.caret.Min.Y, "w": f.caret.Dx(), "h": f.caret.Dy(), "base": f.base},
			"drawn": !f.caret.Empty(),
		})
	}
	if !ok || f.caret.Empty() {
		return
	}
	s := b.gio.shaper
	s.LayoutString(text.Parameters{Font: fontOf(fs.typeface), PxPerEm: fixed.Int26_6(m.ppem * 64), MaxWidth: 1 << 20}, string(held))
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
	fg, bg := preeditColors(f, b.cfg.theme.FG, b.cfg.theme.BG)
	fillRect(ops, box, bg)
	off := op.Offset(image.Pt(box.Min.X, box.Min.Y+f.base)).Push(ops)
	countTransform()
	countGlyphPath()
	outline := clip.Outline{Path: s.Shape(glyphs)}.Op().Push(ops)
	paint.ColorOp{Color: fg}.Add(ops)
	paint.PaintOp{}.Add(ops)
	outline.Pop()
	s.Bitmaps(glyphs).Add(ops)
	off.Pop()
	lw := max(1, int(m.scale+0.5))
	fillRect(ops, image.Rect(box.Min.X, box.Max.Y-lw, box.Max.X, box.Max.Y), fg)
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

// askToClose answers the window's close button: the program decides on its loop (WithQuit: it may
// ask about unsaved work first), so the window stays open for now, and true says to abort the
// close. Stop ends the queue before it closes the window, and that close is the program's own: it
// goes through. Before the App reads events there is no one to ask, and the window closes.
func (b *Backend) askToClose() bool {
	if !b.forwarding.Load() || b.q.ended() {
		return false
	}
	b.q.push(tui.CloseRequestEvent{})
	return true
}
