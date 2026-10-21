package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// The FILE PANES: how every part of every file view is framed and dressed.

// FilePaneStyles are the looks the file views paint. Surface is every pane's
// interior. Cursor is the list's row while the list has the keyboard,
// CursorBlurred the same row once the keyboard has moved elsewhere. Border and
// FocusedBorder frame each pane, without and with the keyboard. Gap is the
// space between two panes, which belongs to whatever holds them — a dialog's
// card — and so is painted in its colour, not left as the terminal's.
type FilePaneStyles struct {
	Surface, Cursor, CursorBlurred, Border, FocusedBorder, Gap style.Style
}

// newFilePane frames one part of a file view, titled. Every pane of every file
// view is framed by this, so they cannot come to look different.
//
// PADDED: a column of room either side, so a name does not run into the frame.
// The frame carries the focus — Box lights its border while the keyboard is
// inside — so the pane in use is named by its own border.
func newFilePane(child tui.Component, title string, st FilePaneStyles, styled bool) *Box {
	b := NewBox(child, WithTitle(title))
	stylePane(b, st, styled)
	return b
}

// stylePane dresses a pane's frame: its interior on Surface, the border in
// Border's colour, and in FocusedBorder's while the keyboard is inside.
func stylePane(b *Box, st FilePaneStyles, styled bool) {
	base := style.New().Padding(0, 1)
	focused := style.Style{}
	if styled {
		// A look that names no colour leaves golib's border token in place.
		base = st.Surface.Padding(0, 1)
		if c, ok := st.Border.GetForeground(); ok {
			base = base.BorderForeground(c)
		}
		if c, ok := st.FocusedBorder.GetForeground(); ok {
			focused = style.New().BorderForeground(c)
		}
	}
	b.WithStyle(base)
	b.WithFocusedStyle(focused)
}
