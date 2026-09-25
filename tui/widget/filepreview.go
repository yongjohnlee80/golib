package widget

import (
	"bytes"
	"io"
	"io/fs"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
)

// FILE PREVIEW — a file's text, framed, for reading.
//
//	┌ Preview ───────┐
//	│ # Groceries    │   read-only, and a tab stop: the keyboard can move in
//	│ - milk         │   and scroll through the whole file with the editor's
//	│ - eggs         │   motions (j/k, gg/G, Ctrl-d/u)
//	└────────────────┘
//
// Its own widget, not a part of a file dialog: anything that shows a file
// beside a list of them — a dialog, an explorer, a picker — shows it with
// this. It reads a file only as far as maxPreview, and says so rather than
// showing noise for a folder, an unreadable file or a binary one.

// maxPreview bounds how much of a file is read to show it: the whole of any
// ordinary text file, and never enough to stall on a large one.
const maxPreview = 1 << 20

// FilePreview shows one file's text.
type FilePreview struct {
	Base
	view *Editor
	box  *Box
}

// NewFilePreview builds an empty preview, dressed like the panes beside it.
func NewFilePreview(st FilePaneStyles, styled bool) *FilePreview {
	opts := []EditorOption{WithEditorWrap(WrapSoft)}
	if styled {
		opts = append(opts, WithEditorStyles(TextInputStyles{Text: st.Surface}))
	}
	// A READ-ONLY EDITOR rather than a Text: a pane the keyboard can enter
	// and read the whole file in. It does not consume Escape when idle, so
	// Escape still reaches a dialog around it.
	p := &FilePreview{view: NewEditor(opts...)}
	p.view.SetReadOnly(true)
	p.box = newFilePane(p.view, "Preview", st, styled)
	return p
}

// Init mounts the frame.
func (p *FilePreview) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	ctx.Mount(p.box)
}

// Layout fills what it is given.
func (p *FilePreview) Layout(c tui.Constraints) tui.Size {
	sz := c.Constrain(tui.Size{W: boundedMax(c.MaxW, 40), H: boundedMax(c.MaxH, 12)})
	ctx := p.Context()
	ctx.LayoutChild(p.box, tui.Tight(sz))
	ctx.PlaceChild(p.box, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (p *FilePreview) Render(tui.Surface)         {}
func (p *FilePreview) HandleEvent(tui.Event) bool { return false }

// AcceptsFocus reports that the frame is not a tab stop; the text is.
func (p *FilePreview) AcceptsFocus() bool { return false }

// Show previews an fs path of src; folder says it is a folder and has no text
// to show. Any fs.FS will do — the preview reads a remote file as it reads a
// local one.
func (p *FilePreview) Show(src FileSource, path string, folder bool) {
	p.view.SetValue(previewOf(src.or().FS, path, folder))
}

// Text is what the preview holds.
func (p *FilePreview) Text() string { return p.view.Value() }

// Focused reports whether the preview has the keyboard.
func (p *FilePreview) Focused() bool {
	ctx := p.view.Context()
	return ctx != nil && ctx.Focused()
}

// Hint is the keys the preview answers to, for a footer.
func (p *FilePreview) Hint() string { return "j/k:scroll  gg/G:top/end" }

// previewOf is what a preview shows for a path.
func previewOf(fsys fs.FS, path string, folder bool) string {
	if folder {
		return "(folder)"
	}
	f, err := fsys.Open(path)
	if err != nil {
		return "(cannot read: " + err.Error() + ")"
	}
	defer f.Close()
	buf := make([]byte, maxPreview)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "(cannot read: " + err.Error() + ")"
	}
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 || !utf8.Valid(trimPartialRune(buf)) {
		return "(binary file — no preview)"
	}
	if n == 0 {
		return "(empty file)"
	}
	return string(buf)
}

// trimPartialRune drops a rune cut in half by the read limit, so a text file
// longer than the limit is not mistaken for a binary one.
func trimPartialRune(b []byte) []byte {
	for i := 0; i < utf8.UTFMax && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}
