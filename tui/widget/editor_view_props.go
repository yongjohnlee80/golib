package widget

import "github.com/yongjohnlee80/golib/tui/style"

// AN EDITOR'S VIEW PROPERTIES, as interfaces: what a declarative layer (tui/decl) sets on any editor
// widget that has them, the cell Editor and a native one alike, without naming either type.

// LineNumberer shows each line's number in a gutter, in a colour of its own.
type LineNumberer interface {
	SetLineNumbers(on bool)
	SetLineNumberColor(c style.Color)
}

// Ruler draws a vertical guide at a column (1-based), where text is meant to wrap; 0 for none.
type Ruler interface{ SetRuler(col int) }

// Wrapper wraps long lines, or not.
type Wrapper interface{ SetWrap(m WrapMode) }

// CursorColorer draws the text cursor in a colour of its own.
type CursorColorer interface{ SetCursorColor(c style.Color) }

// ContextMenuEnabler turns the right-click menu on or off.
type ContextMenuEnabler interface{ SetContextMenu(on bool) }

// ViewChooser chooses the view: Raw, or Rendered where it is available.
type ViewChooser interface{ SetView(v EditorView) }

var (
	_ LineNumberer       = (*Editor)(nil)
	_ Ruler              = (*Editor)(nil)
	_ Wrapper            = (*Editor)(nil)
	_ CursorColorer      = (*Editor)(nil)
	_ ContextMenuEnabler = (*Editor)(nil)
	_ ViewChooser        = (*Editor)(nil)
)
