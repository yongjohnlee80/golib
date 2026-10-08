package widget

import (
	"fmt"
	"image/color"
	"strconv"

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

	caret    style.Color // the caret's colour (SetCursorColor); the default is the text's
	caretSet bool

	wrap      tuiwidget.WrapMode // WrapNone (the default) or WrapSoft: the Raw view's long lines
	numbers   bool               // a gutter of line numbers at the left
	numberCol style.Color        // the numbers' colour (SetLineNumberColor); muted otherwise
	numberSet bool
	ruler     int         // a guide at this column, 1-based, in the Raw view; 0 for none
	page      style.Style // the cells' look the view reads its colours from (SetPageStyle)
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
	wrap     tuiwidget.WrapMode
	numbers  bool
	ruler    int
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

// WithWrap selects how the Raw view shows a line wider than the editor: tuiwidget.WrapNone (the
// default, as the tui Editor's) keeps it on one row and scrolls sideways; tuiwidget.WrapSoft wraps
// it at the editor's width. The Rendered view always wraps: it is prose, read, not columns.
func WithWrap(m tuiwidget.WrapMode) EditorOption { return func(c *editorConfig) { c.wrap = m } }

// WithLineNumbers shows each line's number in a gutter at the left.
func WithLineNumbers(v bool) EditorOption { return func(c *editorConfig) { c.numbers = v } }

// WithRuler marks column col (1-based) of the Raw view with a vertical guide; 0 draws none.
func WithRuler(col int) EditorOption { return func(c *editorConfig) { c.ruler = max(col, 0) } }

// NewEditor is an Editor.
func NewEditor(opts ...EditorOption) *Editor {
	var cfg editorConfig
	for _, o := range opts {
		o(&cfg)
	}
	e := &Editor{core: tuiwidget.NewEditorCore(cfg.core...), render: cfg.render, fontSize: cfg.fontSize,
		wrap: tuiwidget.WrapNone, numbers: cfg.numbers, ruler: cfg.ruler}
	if cfg.wrap == tuiwidget.WrapSoft {
		e.wrap = tuiwidget.WrapSoft
	}
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
	// Ctrl+T, or whatever the keymap binds ActToggleRendered to: with no renderer there is no
	// Rendered view, so the key bubbles.
	e.core.SetToggleRendered(func() bool {
		if e.render == nil {
			return false
		}
		e.toggleMode()
		return true
	})
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
	e.layout.scroll, e.layout.left = 0, 0
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

// SetContextMenuRows gives the right-click menu build's rows, built each time it opens, and turns
// it on: what WithContextMenu does, for an Editor something else constructed (a QML style). nil
// build is the stock rows (Undo, Redo, Copy, Cut, Paste). A row's CoreMenuAction runs on the
// editor's core when the row is chosen.
func (e *Editor) SetContextMenuRows(build func(c *tuiwidget.EditorCore) []tuiwidget.MenuItemModel) {
	if build == nil {
		e.menu.SetRows(nil)
	} else {
		e.menu.SetRows(func() []tuiwidget.MenuItemModel { return build(e.core) })
	}
	e.menu.SetEnabled(true)
}

// SetKeyset changes the editing profile (Vim, Nano, Standard).
func (e *Editor) SetKeyset(ks tuiwidget.Keyset) { e.core.SetKeyset(ks); e.body.MarkDirty() }

// SetReadOnly makes the editor a viewer: motions and yank only.
func (e *Editor) SetReadOnly(v bool) { e.core.SetReadOnly(v); e.body.MarkDirty() }

// SetValue replaces the text, as a load does, and scrolls to its top.
func (e *Editor) SetValue(s string) {
	e.core.SetValue(s)
	e.layout.scroll = 0
	e.body.MarkDirty()
}

// SetCursorPosition puts the cursor pos characters from the start, a line break one.
func (e *Editor) SetCursorPosition(pos int) { e.core.SetCursorPosition(pos); e.body.MarkDirty() }

// SetCursorColor colours the caret; the default colour is the text's.
func (e *Editor) SetCursorColor(c style.Color) {
	e.caret, e.caretSet = c, !c.IsDefault()
	e.body.MarkDirty()
}

// SetWrap selects tuiwidget.WrapNone or tuiwidget.WrapSoft while the editor runs, as WithWrap
// does at construction; the cursor stays where it is.
func (e *Editor) SetWrap(m tuiwidget.WrapMode) {
	if m != tuiwidget.WrapNone && m != tuiwidget.WrapSoft {
		panic(fmt.Sprintf("gui/widget: Editor.SetWrap: mode %d is not WrapNone or WrapSoft", m))
	}
	if e.wrap == m {
		return
	}
	e.wrap = m
	e.layout.invalidate()
	e.layout.left = 0
	if ln, col := e.core.Line(); e.layout.sh != nil {
		e.layout.Reveal(ln, col)
	}
	e.body.MarkDirty()
}

// Wrap is how the Raw view shows long lines.
func (e *Editor) Wrap() tuiwidget.WrapMode { return e.wrap }

// SetLineNumbers shows or hides the gutter of line numbers.
func (e *Editor) SetLineNumbers(v bool) {
	if e.numbers == v {
		return
	}
	e.numbers = v
	e.layout.invalidate()
	e.body.MarkDirty()
}

// LineNumbers reports whether the gutter shows.
func (e *Editor) LineNumbers() bool { return e.numbers }

// GutterWidth is the columns the line numbers take, the gap after them included, as the tui
// Editor counts them: the widest number, four digits at least, and two blank columns; 0 while
// they are hidden.
func (e *Editor) GutterWidth() int {
	if !e.numbers {
		return 0
	}
	return max(len(strconv.Itoa(e.core.LineCount())), 4) + gutterGap
}

// gutterGap is the blank columns between the line numbers and the text.
const gutterGap = 2

// SetLineNumberColor gives the line numbers a colour of their own; muted otherwise.
func (e *Editor) SetLineNumberColor(c style.Color) {
	e.numberCol, e.numberSet = c, !c.IsDefault()
	e.body.MarkDirty()
}

// SetRuler moves the Raw view's guide to column col (1-based); 0 removes it.
func (e *Editor) SetRuler(col int) {
	e.ruler = max(col, 0)
	e.body.MarkDirty()
}

// Ruler is the guide's column; 0 for none.
func (e *Editor) Ruler() int { return e.ruler }

// SetPageStyle is the look of the cells under the text, which the view takes its text and page
// colours from: a palette's text on base. The zero Style is the terminal theme's default.
func (e *Editor) SetPageStyle(st style.Style) {
	e.page = st
	e.layout.invalidate()
	e.body.MarkDirty()
}

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
	t.LineNumbers = t.Muted
	if e.caretSet {
		if c, ok := colorOf(e.caret, th, dark); ok {
			t.Caret = c
		}
	}
	if th != nil {
		if c, ok := colorOf(th.Color(style.TokenAccent), th, dark); ok {
			t.Accent = c
		}
		if c, ok := colorOf(th.Color(style.TokenTextMuted), th, dark); ok {
			t.Muted, t.LineNumbers = c, c
		}
	}
	if e.numberSet {
		if c, ok := colorOf(e.numberCol, th, dark); ok {
			t.LineNumbers = c
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
