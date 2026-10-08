package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// Editor is a text editor in a Panel: the tui Editor's behaviour (its EditorCore: modes,
// keymaps, undo, the register, highlighting) over a layout in pixels. Raw draws the text in
// monospace with its syntax colours; Rendered draws it through a Renderer, such as
// MarkdownRenderer. The title bar's [ Raw | Rendered ] switch and Ctrl+T change the mode.
type Editor struct {
	panel *Panel // a field, not embedded: Panel's methods call each other on their receiver

	core   *tuiwidget.EditorCore
	body   *editorBody
	layout *pixelLayout
	menu   *tuiwidget.EditorMenu
	sw     *modeSwitch

	mode     EditorMode
	render   Renderer
	fontSize float32
}

// EditorOption sets up an Editor under construction.
type EditorOption func(*editorConfig)

type editorConfig struct {
	core     []tuiwidget.CoreOption
	render   Renderer
	mode     EditorMode
	menu     bool
	rows     func(*tuiwidget.EditorCore) []tuiwidget.MenuItemModel
	panel    []PanelOption
	fontSize float32
}

// WithRenderer gives the Editor a Rendered mode drawn by r.
func WithRenderer(r Renderer) EditorOption { return func(c *editorConfig) { c.render = r } }

// WithCore configures the editor's behaviour, as the tui Editor's options do.
func WithCore(opts ...tuiwidget.CoreOption) EditorOption {
	return func(c *editorConfig) { c.core = append(c.core, opts...) }
}

// WithContextMenu turns the right-click menu on, with build's rows; nil build: the stock rows
// (Undo, Redo, Copy, Cut, Paste).
func WithContextMenu(build func(c *tuiwidget.EditorCore) []tuiwidget.MenuItemModel) EditorOption {
	return func(c *editorConfig) { c.menu, c.rows = true, build }
}

// WithMode starts the Editor in m; Rendered without a renderer stays Raw.
func WithMode(m EditorMode) EditorOption { return func(c *editorConfig) { c.mode = m } }

// WithPanel configures the Editor's Panel: its title, and whether it moves, resizes or closes.
func WithPanel(opts ...PanelOption) EditorOption {
	return func(c *editorConfig) { c.panel = append(c.panel, opts...) }
}

// WithFontSize sets the text's size in logical pixels; 0 follows the cells' height.
func WithFontSize(px float32) EditorOption { return func(c *editorConfig) { c.fontSize = px } }

// NewEditor is an Editor.
func NewEditor(opts ...EditorOption) *Editor {
	var cfg editorConfig
	for _, o := range opts {
		o(&cfg)
	}
	e := &Editor{core: tuiwidget.NewEditorCore(cfg.core...), render: cfg.render, fontSize: cfg.fontSize}
	e.layout = newPixelLayout(e)
	e.body = &editorBody{e: e}
	e.sw = &modeSwitch{e: e}
	e.menu = tuiwidget.NewEditorMenu(e.body, e.core)
	if cfg.rows != nil {
		build := cfg.rows
		e.menu.SetRows(func() []tuiwidget.MenuItemModel { return build(e.core) })
	}
	e.menu.SetEnabled(cfg.menu)
	e.panel = NewPanel(e.body, append([]PanelOption{TitleLeading(e.sw)}, cfg.panel...)...)
	e.SetMode(cfg.mode)
	return e
}

var (
	_ tui.Component      = (*Editor)(nil)
	_ tui.NativeReporter = (*Editor)(nil)
	_ tui.NativeScoper   = (*Editor)(nil)
	_ tui.Hideable       = (*Editor)(nil)
)

// Panel is the window the editor sits in: its title bar, and where it is in a Float.
func (e *Editor) Panel() *Panel { return e.panel }

// The Editor is its Panel to tui: the Panel lays out, paints and takes the title bar's input,
// with its children mounted under the Editor.

func (e *Editor) Init(ctx *tui.Context)             { e.panel.Init(ctx) }
func (e *Editor) Layout(c tui.Constraints) tui.Size { return e.panel.Layout(c) }
func (e *Editor) Render(s tui.Surface)              { e.panel.Render(s) }
func (e *Editor) HandleEvent(ev tui.Event) bool     { return e.panel.HandleEvent(ev) }
func (e *Editor) NativeView() (any, bool)           { return e.panel.NativeView() }
func (e *Editor) NativeScope() tui.NativeScope      { return e.panel.NativeScope() }
func (e *Editor) Visible() bool                     { return e.panel.Visible() }

// Core is the editor's behaviour: its text, cursor, modes and keymap.
func (e *Editor) Core() *tuiwidget.EditorCore { return e.core }

// Mode is how the text is drawn.
func (e *Editor) Mode() EditorMode { return e.mode }

// SetMode draws the text Raw or Rendered; Rendered without a renderer stays Raw.
func (e *Editor) SetMode(m EditorMode) {
	if m == Rendered && e.render == nil {
		m = Raw
	}
	if m == e.mode && e.layout.blocks != nil {
		return
	}
	e.mode = m
	e.layout.invalidate()
	e.layout.scroll = 0
	if ln, col := e.core.Line(); e.layout.sh != nil {
		e.layout.Reveal(ln, col)
	}
	e.body.MarkDirty()
	e.sw.MarkDirty()
}

func (e *Editor) toggleMode() {
	if e.mode == Raw {
		e.SetMode(Rendered)
	} else {
		e.SetMode(Raw)
	}
}

// SetContextMenu turns the right-click menu on or off.
func (e *Editor) SetContextMenu(on bool) { e.menu.SetEnabled(on) }

// theme is what the text is drawn with: the colours of the cells under it, the tui theme's
// accent and muted text, and fonts sized to the cells unless WithFontSize set one.
func (e *Editor) theme(fg, bg color.NRGBA, cell gui.Size, th *style.Theme) Theme {
	size := e.fontSize
	if size <= 0 {
		size = max(cell.H*0.72, 10)
	}
	t := Theme{Text: fg, Background: bg,
		Muted:          mix(fg, bg, 0.55),
		Accent:         color.NRGBA{R: 0x5c, G: 0x9c, B: 0xf5, A: 0xff},
		CodeBackground: mix(fg, bg, 0.08),
		Prose:          gui.Font{Size: size * 1.05},
		Mono:           gui.Font{Family: gui.MonospaceFamily(), Size: size},
	}
	dark := isDark(bg)
	if th != nil {
		if c, ok := colorOf(th.Color(style.TokenAccent), th, dark); ok {
			t.Accent = c
		}
		if c, ok := colorOf(th.Color(style.TokenTextMuted), th, dark); ok {
			t.Muted = c
		}
	}
	return t
}

// modeSwitch is the title bar's [ Raw | Rendered ] control.
type modeSwitch struct {
	tuiwidget.Base
	e *Editor
}

const (
	switchRaw      = "[ Raw "
	switchSep      = "|"
	switchRendered = " Rendered ]"
)

// Layout is the control's width on one row.
func (s *modeSwitch) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: len(switchRaw) + len(switchSep) + len(switchRendered), H: 1})
}

// Render draws the two segments, the current one bold and underlined; Rendered is faint
// without a renderer.
func (s *modeSwitch) Render(sur tui.Surface) {
	on, off := style.New().Bold(true).Underline(true), style.New()
	raw, rendered := on, off
	if s.e.mode == Rendered {
		raw, rendered = off, on
	}
	if s.e.render == nil {
		rendered = rendered.Faint(true)
	}
	x := putString(sur, 0, 0, switchRaw, raw)
	x = putString(sur, x, 0, switchSep, off)
	putString(sur, x, 0, switchRendered, rendered)
}

// HandleEvent switches the mode on a click of a segment.
func (s *modeSwitch) HandleEvent(ev tui.Event) bool {
	m, ok := ev.(tui.MouseEvent)
	if !ok || m.Kind != tui.MousePress || m.Button != tui.MouseLeft || m.Y != 0 {
		return false
	}
	switch {
	case m.X < len(switchRaw):
		s.e.SetMode(Raw)
	case m.X > len(switchRaw):
		s.e.SetMode(Rendered)
	}
	return true
}
