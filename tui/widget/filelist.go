package widget

import (
	"context"
	"os"
	"path"
	"path/filepath"

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
	// onLoad runs as a listing STARTS, before its rows are in: what was asked of the folder being
	// left (a typed path's check) is to be dropped then, not once the new rows arrive.
	onLoad func()
	// onShow runs each time a folder's rows are put in: its view's filter
	// field follows the filter, which a new folder clears
	onShow func()
	// rows is the listing's epoch: each navigation or new source (loadThen) and each listing shown
	// starts one, so an owner hook that moves elsewhere ends the reports still due for the listing it
	// left (C1).
	rows rowsEpoch

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
		WithListOnCursor[fileEntry](func(i int) { l.cursorOn(i) }),
		WithListOnActivate[fileEntry](l.activate),
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

// Init mounts the list; the list reports its cursor and activation itself (WithListOnCursor).
func (l *FileList) Init(ctx *tui.Context) {
	l.Base.Init(ctx)
	ctx.Mount(l.box)
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
	if l.show(got.dir, got.dirs, got.files, r.Err) {
		l.placeWanted()
	}
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
			if l.list.cursor == i {
				l.cursorOn(i) // SetCursor reports no move when the cursor is already there
				return true
			}
			l.list.SetCursor(i)
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

func (l *FileList) cursorOn(i int) (live bool) {
	if i < 0 || i >= len(l.entries) || l.onCursor == nil {
		return true
	}
	e := l.entries[i]
	return l.rows.Survives(func() { l.onCursor(l.pathOf(e), e.dir || e.up) })
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
	// Every navigation and new source starts here, and from here the listing shown is on its way out:
	// an operation still reporting it stops now, not when the new rows arrive (an async read may
	// return before they do).
	l.rows.advance()
	if l.onLoad != nil {
		l.onLoad()
	}
	l.want = want
	l.stopRead()
	ctx := l.Context()
	if !l.src.async() {
		l.loading = ""
		dirs, files, err := l.src.readDir(context.Background(), dir)
		if l.show(dir, dirs, files, err) {
			l.placeWanted()
		}
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

// show puts a folder's rows in the list, the filter cleared and the cursor on
// the first; err says the folder could not be read.
func (l *FileList) show(dir string, dirs, files []fileEntry, err error) (live bool) {
	l.rows.advance() // a listing arrived: the one shown is gone, whatever loadThen began
	l.dir = dir
	var entries []fileEntry
	if dir != "." {
		entries = append(entries, fileEntry{up: true})
	}
	l.title = l.src.Rooted(dir)
	l.unreadable = err != nil
	l.all = append(append(entries, dirs...), files...)
	l.filter = ""
	l.putRows(l.all)
	l.fitTitle()
	// The owner is told last, each hook only while this listing is still the one shown: a hook that
	// moves to another folder has told the owner of that one, and nothing of this one may follow.
	if l.onShow != nil && !l.rows.Survives(l.onShow) {
		return false
	}
	if l.onDir != nil && !l.rows.Survives(func() { l.onDir(l.src.Rooted(dir)) }) {
		return false
	}
	// SetItems put the cursor on the first row without telling anyone, so whoever follows the cursor
	// is told here.
	return l.cursorOn(0)
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
	rows := l.all
	if text != "" {
		rows = nil
		for _, e := range l.all {
			if e.up || matchFold(e.name, text) {
				rows = append(rows, e)
			}
		}
	}
	l.putRows(rows)
	l.cursorOn(0)
}

// putRows puts rows in the list: a folder shown, or a filter applied. The list's cursor is on the
// first row, untold. A filter does not end the listing's epoch: the folder is the same, and the
// filter reports its own first row.
func (l *FileList) putRows(rows []fileEntry) {
	l.entries = rows
	l.list.SetItems(rows)
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
