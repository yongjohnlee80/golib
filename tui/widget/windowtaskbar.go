package widget

import "github.com/yongjohnlee80/golib/tui"

// WindowTaskbar is an optional keyboard/pointer restore UI over MinimizedWindows.
// Model observation schedules an owner-bound update; it never removes the activating
// Button inline or mutates children during Layout.
type WindowTaskbar struct {
	tui.MultiChild
	model          *MinimizedWindows
	buttons        map[tui.NodeID]*Button
	entries        []MinimizedWindow
	caption        func(MinimizedWindow) string
	buttonStyle    *ButtonStyle
	dirty, pending bool
}

// WindowTaskbarOption configures the default collector's separate presentation.
type WindowTaskbarOption func(*WindowTaskbar)

// WithWindowTaskbarCaption replaces the entry label without altering its identity.
func WithWindowTaskbarCaption(fn func(MinimizedWindow) string) WindowTaskbarOption {
	return func(w *WindowTaskbar) { w.caption = fn }
}

// WithWindowTaskbarStyle associates an ordinary Button style with restore controls.
func WithWindowTaskbarStyle(st *ButtonStyle) WindowTaskbarOption {
	return func(w *WindowTaskbar) { w.buttonStyle = st }
}

// NewWindowTaskbar constructs a presentation over an explicitly supplied model.
func NewWindowTaskbar(model *MinimizedWindows, opts ...WindowTaskbarOption) *WindowTaskbar {
	if model == nil {
		panic(fatalOf("widget: NewWindowTaskbar", "nil collector model", "a taskbar presents a caller-owned collector"))
	}
	w := &WindowTaskbar{model: model, buttons: make(map[tui.NodeID]*Button)}
	w.Label("WindowTaskbar")
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}
	return w
}

// Init observes model changes for this mount and creates the current entry controls.
func (w *WindowTaskbar) Init(ctx *tui.Context) {
	w.MultiChild.Init(ctx)
	w.pending = false
	w.rebuildWindowEntries()
	ctx.OnUnmount(w.model.Observe(func() {
		if !ctx.Mounted() {
			return
		}
		w.dirty = true
		if !w.pending {
			w.pending = true
			ctx.Update(func() {
				w.pending = false
				if w.dirty {
					w.rebuildWindowEntries()
					ctx.RequestLayout()
				}
			})
		}
	}))
}

func (w *WindowTaskbar) rebuildWindowEntries() {
	entries := w.model.Entries()
	keep := make(map[tui.NodeID]bool, len(entries))
	for _, e := range entries {
		keep[e.Owner] = true
	}
	for owner, b := range w.buttons {
		if !keep[owner] {
			w.Remove(b)
			delete(w.buttons, owner)
		}
	}
	for i, e := range entries {
		label := e.Title
		if w.caption != nil {
			label = w.caption(e)
		}
		if label == "" {
			label = "Window"
		}
		b := w.buttons[e.Owner]
		if b == nil {
			entry := e
			b = NewButton(label, WithButtonStyle(w.buttonStyle), WithOnActivate(func() { entry.InvokeInput(WindowRestoreAction{}) }))
			w.buttons[e.Owner] = b
			w.Add(b)
		} else {
			b.SetLabel(label)
		}
		w.Move(b, i)
	}
	w.entries, w.dirty = entries, false
}

// HandleEvent leaves input behavior to the actual entry Buttons.
func (w *WindowTaskbar) HandleEvent(ev tui.Event) bool {
	return false
}

// Layout arranges restore buttons in a bounded row, using their own measured widths.
func (w *WindowTaskbar) Layout(c tui.Constraints) tui.Size {
	x, height := 0, 0
	for _, e := range w.entries {
		b := w.buttons[e.Owner]
		sz := w.Ctx().LayoutChild(b, tui.Constraints{MaxW: subFrame(c.MaxW, x), MaxH: min(c.MaxH, 1)})
		w.Ctx().PlaceChild(b, tui.Rect{X: x, W: sz.W, H: sz.H})
		x += sz.W + 1
		height = max(height, sz.H)
	}
	return c.Constrain(tui.Size{W: max(x-1, 0), H: height})
}

// Render delegates all presentation to real Button children.
func (w *WindowTaskbar) Render(tui.Surface) {}
