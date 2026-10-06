package widget

import (
	"bytes"
	"io"
	"io/fs"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/highlight"
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

// FilePreview shows one file's text — highlighted, when it is given a way to
// pick a highlighter by file name ([FilePreview.SetHighlighting]).
type FilePreview struct {
	Base
	view *Editor
	box  *Box

	// forFile picks the highlighter for a file by its name, nil for none;
	// shown is the file on show, "" for a message rather than a file's text.
	forFile func(name string) highlight.Highlighter
	shown   string
	// note is the message the preview shows in place of a file's text (a folder, a binary
	// file), with its argument; zero while it shows a file. It is resolved each layout, so the
	// note reads in the App's language.
	note    tui.Message
	noteArg string
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
	p.box = newFilePane(p.view, "", st, styled)
	p.box.SetTitleMessage(tui.Msg("tui.files.preview"))
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
	// The note is the view's text. Setting it edits the view, which a pure phase must not do,
	// so a note that reads differently in the new language is put in at the commit.
	if p.note != (tui.Message{}) {
		if text := p.noteText(); text != p.view.Value() {
			ctx.AfterLayout("preview-note", func() { p.view.SetValue(text) })
		}
	}
	ctx.LayoutChild(p.box, tui.Tight(sz))
	ctx.PlaceChild(p.box, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (p *FilePreview) Render(tui.Surface)         {}
func (p *FilePreview) HandleEvent(tui.Event) bool { return false }

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the frame is not a tab stop; the
// text is. Absence of the capability says so (tutorial ch. 9).

// Show previews an fs path of src; folder says it is a folder and has no text
// to show. Any fs.FS will do — the preview reads a remote file as it reads a
// local one.
func (p *FilePreview) Show(src FileSource, path string, folder bool) {
	text, note, arg := previewOf(src.or().FS, path, folder)
	p.shown, p.note, p.noteArg = "", note, arg
	if note == (tui.Message{}) {
		p.shown = path
	} else {
		text = p.noteText()
	}
	p.view.SetValue(text)
	p.highlight()
}

// SetHighlighting highlights what the preview shows: forFile picks the
// highlighter for a file by its name — KSyntaxHighlighting's
// definitionForFileName — and styles colour it. A nil forFile is none. A
// message — a folder, a binary file — is never highlighted.
func (p *FilePreview) SetHighlighting(forFile func(name string) highlight.Highlighter, styles SyntaxStyles) {
	p.forFile = forFile
	p.view.WithSyntaxStyles(styles)
	p.highlight()
}

func (p *FilePreview) highlight() {
	var h highlight.Highlighter
	if p.forFile != nil && p.shown != "" {
		h = p.forFile(p.shown)
	}
	p.view.SetHighlighter(h)
}

// Text is what the preview holds.
func (p *FilePreview) Text() string { return p.view.Value() }

// Focused reports whether the preview has the keyboard.
func (p *FilePreview) Focused() bool {
	ctx := p.view.Context()
	return ctx != nil && ctx.Focused()
}

// Hint is the keys the preview answers to, for a footer, in the App's language.
func (p *FilePreview) Hint() string { return p.translate(p.HintMessage()) }

// HintMessage is [FilePreview.Hint] as a catalog message, for a footer that follows the
// language by itself.
func (p *FilePreview) HintMessage() tui.Message { return tui.Msg("tui.files.hint.preview") }

// noteText is the note in the App's language, its argument filled in.
func (p *FilePreview) noteText() string { return withArg(p.translate(p.note), p.noteArg) }

// previewOf is what a preview shows for a path: the file's text, or a note about it (a
// folder, a binary file) with the note's argument.
func previewOf(fsys fs.FS, path string, folder bool) (text string, note tui.Message, arg string) {
	if folder {
		return "", tui.Msg("tui.files.isFolder"), ""
	}
	f, err := fsys.Open(path)
	if err != nil {
		return "", tui.Msg("tui.files.cannotRead"), err.Error()
	}
	defer f.Close()
	buf := make([]byte, maxPreview)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", tui.Msg("tui.files.cannotRead"), err.Error()
	}
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 || !utf8.Valid(trimPartialRune(buf)) {
		return "", tui.Msg("tui.files.binary"), ""
	}
	if n == 0 {
		return "", tui.Msg("tui.files.emptyFile"), ""
	}
	return string(buf), tui.Message{}, ""
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
