package widget

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/vfs"
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
//
// An fs.FS is read on the UI loop, as the local disk always has been, unless
// Async says to read it off the loop. A source that may be slow (a daemon's, a
// remote store) is a [vfs.FS] in VFS instead, which the widgets read OFF the
// loop, with the reading task's context: a listing or a preview that a newer
// one supersedes, or that its dialog's closing ends, is stopped rather than
// only dropped.
type FileSource struct {
	FS fs.FS
	// VFS, when set, is read instead of FS, off the loop: VFS wins when both
	// are set. Its symlinks are listed as files, since vfs reports a link
	// without following it.
	VFS vfs.FS
	// Async reads FS off the loop, as a VFS source is read: a slow mount (NFS,
	// sshfs) no longer holds the loop, and links are followed as on the loop.
	// An fs.FS read takes no context, so a superseded one cannot be stopped:
	// it runs to its end on a worker and its answer is dropped.
	Async bool
	Root  string
	// Home is the rooted folder ~ stands for in a typed path; "" when the source has none, and
	// then a typed ~ is refused rather than guessed.
	Home string
}

// LocalFiles is the local filesystem, rooted at "/", ~ its user's home folder.
func LocalFiles() FileSource {
	home, _ := os.UserHomeDir()
	return FileSource{FS: os.DirFS("/"), Root: "/", Home: home}
}

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

// or returns the source, or the local filesystem when it has none, read off
// the loop when the source said Async.
func (s FileSource) or() FileSource {
	if s.FS == nil && s.VFS == nil {
		local := LocalFiles()
		local.Async = s.Async
		if s.Home != "" {
			local.Home = s.Home
		}
		return local
	}
	return s
}

// async reports whether the source is read off the loop: a VFS one, or an
// Async fs.FS.
func (s FileSource) async() bool { return s.VFS != nil || s.Async }

// isDir reports whether an fs path is a folder, following a link to one in an
// fs.FS, with ctx bounding a VFS read. A Save or Folder view asks it of a typed
// name off the loop for an async source (typedStat).
func (s FileSource) isDir(ctx context.Context, p string) bool {
	if s.VFS != nil {
		fi, err := s.VFS.Stat(ctx, p)
		return err == nil && fi.IsDir()
	}
	fi, err := fs.Stat(s.FS, p)
	return err == nil && fi.IsDir()
}

// readDir lists a folder, its folders and its files each sorted without regard
// to case, with ctx bounding a VFS read.
func (s FileSource) readDir(ctx context.Context, dir string) (dirs, files []fileEntry, err error) {
	if s.VFS != nil {
		infos, err := s.VFS.ReadDir(ctx, dir)
		for _, fi := range infos {
			if fi.IsDir() {
				dirs = append(dirs, fileEntry{name: fi.Name, dir: true})
			} else {
				files = append(files, fileEntry{name: fi.Name})
			}
		}
		sortEntries(dirs)
		sortEntries(files)
		return dirs, files, err
	}
	ds, err := fs.ReadDir(s.FS, dir)
	for _, d := range ds {
		// A LINK to a folder is a folder to the user; asking the source
		// follows it, which DirEntry.IsDir does not.
		if d.IsDir() || (d.Type()&fs.ModeSymlink != 0 && s.isDir(ctx, path.Join(dir, d.Name()))) {
			dirs = append(dirs, fileEntry{name: d.Name(), dir: true})
		} else {
			files = append(files, fileEntry{name: d.Name()})
		}
	}
	sortEntries(dirs)
	sortEntries(files)
	return dirs, files, err
}

func sortEntries(s []fileEntry) {
	sort.Slice(s, func(i, j int) bool { return strings.ToLower(s[i].name) < strings.ToLower(s[j].name) })
}

// open opens a file to read, with ctx bounding a VFS read.
func (s FileSource) open(ctx context.Context, p string) (io.ReadCloser, error) {
	if s.VFS != nil {
		return s.VFS.Open(ctx, p, 0)
	}
	return s.FS.Open(p)
}

// FilePaneStyles are the looks the file views paint. Surface is every pane's
// interior. Cursor is the list's row while the list has the keyboard,
// CursorBlurred the same row once the keyboard has moved elsewhere. Border and
// FocusedBorder frame each pane, without and with the keyboard. Gap is the
// space between two panes, which belongs to whatever holds them — a dialog's
// card — and so is painted in its colour, not left as the terminal's.
type FilePaneStyles struct {
	Surface, Cursor, CursorBlurred, Border, FocusedBorder, Gap style.Style
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

// WithOnDir sets what runs each time the list shows a folder, the first
// included, with the folder as a rooted path: a path field that follows it.
func WithOnDir(fn func(rooted string)) FileListOption {
	return func(l *FileList) { l.onDir = fn }
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
	onDir    func(string)
	// onShow runs each time a folder's rows are put in: its view's filter
	// field follows the filter, which a new folder clears
	onShow func()

	list *List[fileEntry]
	box  *Box
	// title is the folder as it stands, before it is fitted to the pane; unreadable says it
	// could not be read, which the pane's title then says too.
	title      string
	unreadable bool

	// all is the folder's rows, and entries those the filter leaves (all of
	// them with none): what the list shows and the cursor indexes.
	all    []fileEntry
	filter string

	// An async source (VFS, or an Async fs.FS) is listed off the loop: listTask is the listing in
	// flight, loading the folder it reads, and want the name the cursor goes to
	// once it arrives ("" for the first row). A newer load supersedes all three.
	listTask tui.TaskID
	loading  string
	want     string
	// stopList cancels the listing in flight: a newer load, or a source read on
	// the loop taking over, stops it rather than leaving it to finish
	stopList context.CancelFunc
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
		WithEmptyTextMessage[fileEntry](tui.Msg("tui.files.empty")),
	}
	if l.styled {
		listOpts = append(listOpts, WithListStyles[fileEntry](fileListStyles(l.st)))
	}
	l.list = NewList(listOpts...)
	l.box = newFilePane(l.list, l.src.Rooted(l.dir), l.st, l.styled)
	// An async source is listed once the list is mounted, off the loop, so
	// building a dialog never waits on it.
	if !l.src.async() {
		l.load(l.dir)
	}
	return l
}

// fileListStyles are a file list's rows in the view's looks: the cursor on
// Cursor while the list has the keyboard, on CursorBlurred once it has not.
// Not reversed: the looks were chosen as pairs.
func fileListStyles(st FilePaneStyles) ListStyles {
	cursor := st.Cursor.Reverse(false)
	ls := ListStyles{Row: st.Surface, CursorRow: cursor, SelectedRow: st.Surface, CursorSelected: cursor}
	if st.CursorBlurred != (style.Style{}) {
		ls.CursorBlurred = st.CursorBlurred.Reverse(false)
	}
	return ls
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
	if l.src.async() {
		l.loadThen(l.dir, l.want)
	}
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

func (l *FileList) Render(tui.Surface) {}

// HandleEvent applies an async listing when it arrives, unless a newer load has
// superseded it: a superseded task still reports, its ctx cancelled.
func (l *FileList) HandleEvent(ev tui.Event) bool {
	r, ok := ev.(tui.TaskResult)
	if !ok || r.ID != l.listTask || l.loading == "" {
		return ok && r.Owner == l.NodeID()
	}
	got, _ := r.Value.(listing)
	l.loading = ""
	l.stopRead()
	l.show(got.dir, got.dirs, got.files, r.Err)
	l.placeWanted()
	return true
}

// listing is a VFS folder's rows, as its task reads them.
type listing struct {
	dir         string
	dirs, files []fileEntry
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the frame is not a tab stop; its
// list is. Absence of the capability says so (tutorial ch. 9), which is what
// tui.App.HoldsFocusable reads.

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

// SetSource lists another filesystem, from the folder the list was asked to
// open in when the new source has it, else from its root. A listing of the old
// source still on its way is superseded.
func (l *FileList) SetSource(src FileSource) {
	l.src = src.or()
	l.loadThen(l.startDir(), "")
}

// Dir is the folder being listed, as an fs path: the one on its way, while an
// async listing is.
func (l *FileList) Dir() string {
	if l.loading != "" {
		return l.loading
	}
	return l.dir
}

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
	if l.loading != "" {
		return "", false, false // the rows are the last folder's
	}
	i, ok := l.list.Selected()
	if !ok || i < 0 || i >= len(l.entries) {
		return "", false, false
	}
	e := l.entries[i]
	return l.pathOf(e), e.dir || e.up, true
}

// SetCurrent puts the cursor on the entry named name in the folder listed,
// and reports whether there is one.
func (l *FileList) SetCurrent(name string) bool {
	if l.loading != "" {
		l.want = name // placed once the folder's rows are in
		return false
	}
	for i, e := range l.entries {
		if !e.up && e.name == name {
			l.list.SetCursor(i)
			// SetCursor publishes nothing when the cursor is already there.
			l.cursorOn(i)
			return true
		}
	}
	return false
}

// Enter is Enter on the current row: a folder is gone into and reports false;
// a file reports true and is left to the caller.
func (l *FileList) Enter() bool {
	if l.loading != "" {
		return false // nothing on screen is the folder being listed
	}
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

// Hint is the keys the list answers to, for a footer, in the App's language.
func (l *FileList) Hint() string { return l.translate(l.HintMessage()) }

// HintMessage is [FileList.Hint] as a catalog message, for a footer that follows the
// language by itself.
func (l *FileList) HintMessage() tui.Message { return tui.Msg("tui.files.hint.list") }

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

// load lists dir, the cursor on its first row.
func (l *FileList) load(dir string) { l.loadThen(dir, "") }

// loadThen lists dir with the cursor on the row named want ("" for the first).
// A source read on the loop is read here; an async one off it, the old rows
// kept and the title saying it is loading until the listing arrives.
func (l *FileList) loadThen(dir, want string) {
	l.want = want
	l.stopRead()
	ctx := l.Context()
	if !l.src.async() {
		l.loading = ""
		dirs, files, err := l.src.readDir(context.Background(), dir)
		l.show(dir, dirs, files, err)
		l.placeWanted()
		return
	}
	if ctx == nil {
		l.dir = dir // no rows yet: listed at Init
		return
	}
	// The rows on screen stay those of l.dir, the folder they were read from,
	// and none of them can be chosen until the new folder's arrive (Current,
	// Enter), so no old name is ever joined to the new folder.
	l.loading = dir
	l.title = l.src.Rooted(dir)
	l.fitTitle()
	src := l.src
	l.listTask, l.stopList = goRead(ctx, "files.list", func(c context.Context) (any, error) {
		dirs, files, err := src.readDir(c, dir)
		return listing{dir: dir, dirs: dirs, files: files}, err
	})
}

// stopRead cancels the listing in flight, if any.
func (l *FileList) stopRead() {
	if l.stopList != nil {
		l.stopList()
		l.stopList = nil
	}
}

// goRead runs read off the loop as one of the owner's group's tasks: a newer
// one in the group cancels it, as does its owner's unmount, and so does the
// returned stop, which its owner calls when something not run as a task (a
// folder row, an fs.FS source) takes over.
func goRead(ctx *tui.Context, group string, read func(context.Context) (any, error)) (tui.TaskID, context.CancelFunc) {
	own, stop := context.WithCancel(context.Background())
	id := ctx.Go(func(task context.Context) (any, error) {
		c, cancel := context.WithCancel(task)
		defer cancel()
		defer context.AfterFunc(own, cancel)()
		return read(c)
	}, tui.Exclusive(group))
	return id, stop
}

// show puts a folder's rows in the list, the filter cleared and the cursor on
// the first; err says the folder could not be read.
func (l *FileList) show(dir string, dirs, files []fileEntry, err error) {
	l.dir = dir
	var entries []fileEntry
	if dir != "." {
		entries = append(entries, fileEntry{up: true})
	}
	l.title = l.src.Rooted(dir)
	l.unreadable = err != nil
	l.all = append(append(entries, dirs...), files...)
	l.filter = ""
	l.entries = l.all
	l.list.SetItems(l.entries)
	l.list.SetCursor(0)
	l.fitTitle()
	if l.onShow != nil {
		l.onShow()
	}
	if l.onDir != nil {
		l.onDir(l.src.Rooted(dir))
	}
	// SetCursor(0) publishes nothing when the cursor was already on row 0, so
	// whoever follows the cursor is told here.
	l.cursorOn(0)
}

// placeWanted puts the cursor on the row a load asked for, once its rows are in.
func (l *FileList) placeWanted() {
	if want := l.want; want != "" {
		l.want = ""
		l.SetCurrent(want)
	}
}

// Loading reports whether an async listing is on its way.
func (l *FileList) Loading() bool { return l.loading != "" }

// Filter is the text the rows are narrowed to, "" for none.
func (l *FileList) Filter() string { return l.filter }

// SetFilter narrows the rows to the names containing text, without regard to
// case; `..` always stays. "" shows every row. The cursor goes to the first.
func (l *FileList) SetFilter(text string) {
	if text == l.filter {
		return
	}
	l.filter = text
	if text == "" {
		l.entries = l.all
	} else {
		q := strings.ToLower(text)
		l.entries = nil
		for _, e := range l.all {
			if e.up || strings.Contains(strings.ToLower(e.name), q) {
				l.entries = append(l.entries, e)
			}
		}
	}
	l.list.SetItems(l.entries)
	l.list.SetCursor(0)
	l.cursorOn(0)
}

// fitTitle elides the folder from the LEFT, at a "/", to fit the pane.
func (l *FileList) fitTitle() {
	title := l.title
	switch {
	case l.loading != "":
		title = withArg(l.translate(tui.Msg("tui.files.titleLoading")), title)
	case l.unreadable:
		title = withArg(l.translate(tui.Msg("tui.files.titleCannotRead")), title)
	}
	// The border's two corners, and a space either side of the title.
	room := l.width - 4
	measure := tui.StringWidth
	if ctx := l.Context(); ctx != nil {
		measure = ctx.StringWidth
	}
	if l.width > 0 && room > 1 {
		title = elide(title, room, ElidePath, measure)
	}
	l.box.SetTitle(title)
}
