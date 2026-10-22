package widget

import tuiwidget "github.com/yongjohnlee80/golib/tui/widget"

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement.

// EditorMode is the view an Editor draws: its text (Raw) or its Rendered view.
//
// Deprecated: use tuiwidget.EditorView, the view both golib editors draw. Not maintained; removed
// in the next minor version.
type EditorMode = tuiwidget.EditorView

const (
	// Raw draws the text as it is, in the monospace font, with its syntax colours.
	//
	// Deprecated: use tuiwidget.ViewRaw. Not maintained; removed in the next minor version.
	Raw = tuiwidget.ViewRaw
	// Rendered draws the text through the Editor's Renderer, or its document view.
	//
	// Deprecated: use tuiwidget.ViewRendered. Not maintained; removed in the next minor version.
	Rendered = tuiwidget.ViewRendered
)

// WithMode starts the Editor in m.
//
// Deprecated: use WithView. Not maintained; removed in the next minor version.
func WithMode(m EditorMode) EditorOption { return WithView(m) }

// Mode is the view the user chose.
//
// Deprecated: use View. Not maintained; removed in the next minor version.
func (e *Editor) Mode() EditorMode { return e.View() }

// Shown is the view drawn now.
//
// Deprecated: use ShownView. Not maintained; removed in the next minor version.
func (e *Editor) Shown() EditorMode { return e.ShownView() }

// SetMode chooses Raw or Rendered.
//
// Deprecated: use SetView, tuiwidget.ViewChooser's method. Not maintained; removed in the next
// minor version.
func (e *Editor) SetMode(m EditorMode) { e.SetView(m) }
