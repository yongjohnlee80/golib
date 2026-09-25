package widget

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// FILE LIST — a folder's files, and moving between folders.
//
//	┌ …/me/notes ──────┐
//	│ ../              │   `..` first, then folders, then files,
//	│ archive/         │   each sorted without regard to case
//	│ todo.md          │
//	└──────────────────┘
//
// The BASE of the file views. It lists a folder and moves through folders —
// Enter on one goes into it, Enter on `..` goes up — and it says, when asked,
// what is under its cursor. It chooses nothing: a view built from it decides
// what a file under the cursor means. [FileOpenView] puts a preview beside it
// and opens the file; [FileSaveView] puts a name field above it and names one.
//
// It is framed and titled with its folder, elided from the LEFT to fit — the
// end of a path is what tells two folders apart.

// FileSource is a filesystem the file widgets list, and how its paths are
// written for a person.
//
// PLATFORM-INDEPENDENT: the widgets reach files only through io/fs, so a
// FileSource over the local disk, an embedded tree, a zip or a remote store
// (an SFTP or object-store fs.FS) lists and previews the same way. Root is
// what a path is written under — "/" locally, "sftp://host/" remotely — and
// every path the widgets report or accept is in that ROOTED form, so a host
// hears an ordinary absolute path from a local dialog.
type FileSource struct {
	FS   fs.FS
	Root string
}

// LocalFiles is the local filesystem, rooted at "/".
func LocalFiles() FileSource { return FileSource{FS: os.DirFS("/"), Root: "/"} }

// Rooted writes an fs path as a person reads it: "home/me" → "/home/me".
func (s FileSource) Rooted(p string) string {
	if p == "." || p == "" {
		return s.Root
	}
	return s.Root + p
}

// fsPath reads a rooted path back into an fs path, reporting false for one
// outside this source. A relative path is taken as relative to the root.
func (s FileSource) fsPath(rooted string) (string, bool) {
	p := filepath.ToSlash(rooted)
	if s.Root != "" && strings.HasPrefix(p, s.Root) {
		p = strings.TrimPrefix(p, s.Root)
	} else if strings.HasPrefix(p, "/") {
		return "", false
	}
	p = path.Clean("/" + p)[1:]
	if p == "" {
		p = "."
	}
	return p, fs.ValidPath(p)
}

// or returns the source, or the local filesystem when it has none.
func (s FileSource) or() FileSource {
	if s.FS == nil {
		return LocalFiles()
	}
	return s
}

// isDir reports whether an fs path is a folder, following a link to one.
func (s FileSource) isDir(p string) bool {
	fi, err := fs.Stat(s.FS, p)
	return err == nil && fi.IsDir()
}

// FilePaneStyles are the looks the file views paint. Surface is every pane's
// interior. Cursor is the list's row while the list has the keyboard,
// CursorBlurred the same row once the keyboard has moved elsewhere. Border and
// FocusedBorder frame each pane, without and with the keyboard.
type FilePaneStyles struct {
	Surface, Cursor, CursorBlurred, Border, FocusedBorder style.Style
}

// FileListOption configures a FileList under construction.
type FileListOption func(*FileList)

// WithFileListSource sets the filesystem the list reads. Default: the local
// one.
func WithFileListSource(src FileSource) FileListOption {
	return func(l *FileList) { l.src = src }
}

// WithFileListDir sets the folder the list opens in, as a rooted path.
// Default: the working directory, locally; the root, elsewhere.
func WithFileListDir(dir string) FileListOption {
	return func(l *FileList) { l.start = dir }
}

// WithFileListStyles dresses the list.
func WithFileListStyles(st FilePaneStyles) FileListOption {
	return func(l *FileList) { l.st, l.styled = st, true }
}

// WithOnCursor sets what runs when the cursor lands on a row: the row's fs
// path, and whether it is a folder (`..` included).
func WithOnCursor(fn func(path string, folder bool)) FileListOption {
	return func(l *FileList) { l.onCursor = fn }
}

// WithOnFile sets what runs on Enter, or a double click, on a FILE, with its fs
// path. A folder is not reported: the list goes into it.
func WithOnFile(fn func(path string)) FileListOption {
	return func(l *FileList) { l.onFile = fn }
}

// fileEntry is one row of the listing.
type fileEntry struct {
	name string
	dir  bool
	// up is the `..` row.
	up bool
}

func (e fileEntry) label() string {
	switch {
	case e.up:
		return "../"
	case e.dir:
		return e.name + "/"
	}
	return e.name
}

// FileList lists a folder.
type FileList struct {
	Base

	src FileSource
	// start is WithFileListDir's rooted path, read once at construction.
	start string
	// dir is the folder listed, as an fs path.
	dir      string
	entries  []fileEntry
	st       FilePaneStyles
	styled   bool
	onCursor func(string, bool)
	onFile   func(string)

	list *List[fileEntry]
	box  *Box
	// title is the folder as it stands — and why it could not be read, if it
	// could not — before it is fitted to the pane.
	title string
	// width is the pane's width at the last layout, for fitting the title.
	width int
}

// NewFileList lists a folder.
func NewFileList(opts ...FileListOption) *FileList {
	l := &FileList{}
	for _, o := range opts {
		if o != nil {
			o(l)
		}
	}
	l.src = l.src.or()
	l.dir = l.startDir()
	listOpts := []ListOption[fileEntry]{
		WithItems([]fileEntry(nil), fileEntry.label),
		WithEmptyText[fileEntry]("(empty)"),
	}
	if l.styled {
		cursor := l.st.Cursor.Reverse(false)
		ls := ListStyles{Row: l.st.Surface, CursorRow: cursor, SelectedRow: l.st.Surface, CursorSelected: cursor}
		if l.st.CursorBlurred != (style.Style{}) {
			ls.CursorBlurred = l.st.CursorBlurred.Reverse(false)
		}
		listOpts = append(listOpts, WithListStyles[fileEntry](ls))
	}
	l.list = NewList(listOpts...)
	l.box = newFilePane(l.list, l.src.Rooted(l.dir), l.st, l.styled)
	l.load(l.dir)
	return l
}

// newFilePane frames one part of a file view, titled. Every pane of every file
// view is framed by this, so they cannot come to look different.
//
// PADDED: a column of room either side, so a name does not run into the frame.
// The frame carries the focus — Box lights its border while the keyboard is
// inside — so the pane in use is named by its own border.
func newFilePane(child tui.Component, title string, st FilePaneStyles, styled bool) *Box {
	base := style.New().Padding(0, 1)
	opts := []BoxOption{WithTitle(title)}
	if styled {
		base = st.Surface.Padding(0, 1).BorderForeground(foregroundOf(st.Border))
		opts = append(opts, WithFocusedStyle(style.New().BorderForeground(foregroundOf(st.FocusedBorder))))
	}
	return NewBox(child, append(opts, WithStyle(base))...)
}

// foregroundOf is a look's foreground, which is what a border is drawn in.
func foregroundOf(st style.Style) style.Color {
	c, _ := st.GetForeground()
	return c
}

// Init mounts the list and follows its cursor.
func (l *FileList) Init(ctx *tui.Context) {
	l.Base.Init(ctx)
	ctx.Mount(l.box)
	mine := func(owner tui.NodeID) bool { return owner == l.list.NodeID() }
	tui.SubscribeScoped(ctx, func(ev SelectionChangedEvent) {
		if mine(ev.Owner) {
			l.cursorOn(ev.Index)
		}
	})
	tui.SubscribeScoped(ctx, func(ev ActivateEvent) {
		if mine(ev.Owner) {
			l.activate(ev.Index)
		}
	})
}

// Layout fills what it is given.
func (l *FileList) Layout(c tui.Constraints) tui.Size {
	sz := c.Constrain(tui.Size{W: boundedMax(c.MaxW, 40), H: boundedMax(c.MaxH, 12)})
	ctx := l.Context()
	ctx.LayoutChild(l.box, tui.Tight(sz))
	ctx.PlaceChild(l.box, tui.Rect{W: sz.W, H: sz.H})
	l.width = sz.W
	l.fitTitle()
	return sz
}

func (l *FileList) Render(tui.Surface)         {}
func (l *FileList) HandleEvent(tui.Event) bool { return false }

// AcceptsFocus reports that the frame is not a tab stop; its list is.
func (l *FileList) AcceptsFocus() bool { return false }

// Focus gives the list the keyboard.
func (l *FileList) Focus() {
	if ctx := l.Context(); ctx != nil {
		ctx.FocusComponent(l.list)
	}
}

// Focused reports whether the list has the keyboard.
func (l *FileList) Focused() bool {
	ctx := l.list.Context()
	return ctx != nil && ctx.Focused()
}

// startDir is where the list opens: the rooted path it was given, else the
// working directory when the source is the local disk, else the root.
func (l *FileList) startDir() string {
	start := l.start
	if start == "" && l.src.Root == "/" {
		start, _ = os.Getwd()
	}
	if start != "" {
		if abs, err := filepath.Abs(start); err == nil && l.src.Root == "/" {
			start = abs
		}
		if p, ok := l.src.fsPath(start); ok {
			return p
		}
	}
	return "."
}

// Source is the filesystem the list reads.
func (l *FileList) Source() FileSource { return l.src }

// Dir is the folder being listed, as an fs path.
func (l *FileList) Dir() string { return l.dir }

// SetDir lists another folder, given as a rooted path; one outside the source
// is ignored. Listing the same one again reads it afresh.
func (l *FileList) SetDir(rooted string) {
	if p, ok := l.src.fsPath(rooted); ok {
		l.load(p)
	}
}

// Current is the row under the cursor: its fs path, and whether it is a
// folder.
func (l *FileList) Current() (path string, folder, ok bool) {
	i, ok := l.list.Selected()
	if !ok || i < 0 || i >= len(l.entries) {
		return "", false, false
	}
	e := l.entries[i]
	return l.pathOf(e), e.dir || e.up, true
}

// Enter is Enter on the current row: a folder is gone into and reports false;
// a file reports true and is left to the caller.
func (l *FileList) Enter() bool {
	i, ok := l.list.Selected()
	if !ok || i < 0 || i >= len(l.entries) {
		return false
	}
	e := l.entries[i]
	if e.up || e.dir {
		l.load(l.pathOf(e))
		return false
	}
	return true
}

// Hint is the keys the list answers to, for a footer.
func (l *FileList) Hint() string { return "↑↓:move  Enter:open folder" }

func (l *FileList) pathOf(e fileEntry) string {
	if e.up {
		return path.Dir(l.dir)
	}
	return path.Join(l.dir, e.name)
}

func (l *FileList) cursorOn(i int) {
	if i < 0 || i >= len(l.entries) || l.onCursor == nil {
		return
	}
	e := l.entries[i]
	l.onCursor(l.pathOf(e), e.dir || e.up)
}

// activate is Enter, or a double click, on a row.
func (l *FileList) activate(i int) {
	if i < 0 || i >= len(l.entries) {
		return
	}
	if l.Enter() && l.onFile != nil {
		l.onFile(l.pathOf(l.entries[i]))
	}
}

// load lists dir.
func (l *FileList) load(dir string) {
	l.dir = dir
	var entries []fileEntry
	if dir != "." {
		entries = append(entries, fileEntry{up: true})
	}
	l.title = l.src.Rooted(dir)
	ds, err := fs.ReadDir(l.src.FS, dir)
	if err != nil {
		l.title += " — cannot read"
	}
	var dirs, files []fileEntry
	for _, d := range ds {
		// A LINK to a folder is a folder to the user; asking the source
		// follows it, which DirEntry.IsDir does not.
		if d.IsDir() || (d.Type()&fs.ModeSymlink != 0 && l.src.isDir(path.Join(dir, d.Name()))) {
			dirs = append(dirs, fileEntry{name: d.Name(), dir: true})
		} else {
			files = append(files, fileEntry{name: d.Name()})
		}
	}
	byName := func(s []fileEntry) {
		sort.Slice(s, func(i, j int) bool { return strings.ToLower(s[i].name) < strings.ToLower(s[j].name) })
	}
	byName(dirs)
	byName(files)
	l.entries = append(append(entries, dirs...), files...)
	l.list.SetItems(l.entries)
	l.list.SetCursor(0)
	l.fitTitle()
	// SetCursor(0) publishes nothing when the cursor was already on row 0, so
	// whoever follows the cursor is told here.
	l.cursorOn(0)
}

// fitTitle elides the folder from the LEFT to fit the pane.
func (l *FileList) fitTitle() {
	title := l.title
	// The border's two corners, and a space either side of the title.
	room := l.width - 4
	measure := tui.StringWidth
	if ctx := l.Context(); ctx != nil {
		measure = ctx.StringWidth
	}
	if l.width > 0 && room > 1 && measure(title) > room {
		rs := []rune(title)
		for len(rs) > 0 && measure("…"+string(rs)) > room {
			rs = rs[1:]
		}
		title = "…" + string(rs)
	}
	l.box.SetTitle(title)
}
