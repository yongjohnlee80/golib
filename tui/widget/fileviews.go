package widget

import (
	"context"
	"io/fs"
	"path"
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
)

// FILE VIEWS — the bodies of an Open and a Save dialog, composed from the
// file widgets rather than written again for each.
//
//	FileOpenView                         FileSaveView
//	┌ …/notes ───────┐ ┌ Preview ────┐   ┌ File name ────────────────────┐
//	│ ../            │ │ # Groceries │   │ todo.md                       │
//	│ todo.md        │ │ - milk      │   └───────────────────────────────┘
//	└────────────────┘ └─────────────┘   ┌ …/notes ──────────────────────┐
//	   FileList          FilePreview     │ ../                           │
//	                    (optional)       │ todo.md                       │
//	                                     └───────────────────────────────┘
//	                                       a FileOpenView, preview off
//
// A FileSaveView IS a name field over a FileOpenView, so a Save dialog lists
// and moves through folders — and previews, if asked — exactly as an Open
// dialog does, because it is the same code doing it.
//
// Both are a [FileChooser], which is all a dialog needs of either: a dialog
// asks the chooser whether the selection is a choice, and never knows which
// kind of chooser it holds.

// FileChooser is what a file dialog needs from its body.
type FileChooser interface {
	tui.Component
	// Confirm is the one decision: is the selection, as it stands, a choice?
	// A folder is not — confirming one goes into it.
	Confirm() bool
	// Selected is the file that would be chosen now, as a ROOTED path — an
	// ordinary absolute path locally — or "".
	Selected() string
	// Dir is the folder being shown, rooted; SetDir shows another, or the
	// same one read afresh.
	Dir() string
	SetDir(rooted string)
	// Select places a selection, given rooted: its folder is listed and the
	// file made the one chosen now — the cursor on it, or the name field
	// holding its name — so "Save As" can start from the file being edited.
	Select(rooted string)
	// FocusInitial gives the keyboard to where a user starts.
	FocusInitial()
	// Hint is the keys the part with the keyboard answers to.
	Hint() string
}

var (
	_ FileChooser = (*FileOpenView)(nil)
	_ FileChooser = (*FileSaveView)(nil)
)

// FileViewOption configures either file view. ONE option type for both, so a
// Save view is configured with the same words as the Open view inside it.
type FileViewOption func(*fileViewConfig)

type fileViewConfig struct {
	src        FileSource
	dir        string
	st         FilePaneStyles
	styled     bool
	preview    bool
	previewSet bool
	onChoose   func()
	onHint     func(string)
	fields     []tui.Component
	// filter gives the view a `/` filter over its listing: an Open view's,
	// not the Save and Folder views' listings
	filter bool
}

func fileViewConfigOf(opts []FileViewOption) fileViewConfig {
	var c fileViewConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// WithFileViewSource sets the filesystem a view lists: local by default, or
// any fs.FS — a remote one included — with the root its paths are written
// under.
func WithFileViewSource(src FileSource) FileViewOption {
	return func(c *fileViewConfig) { c.src = src }
}

// WithFileViewDir sets the folder a view opens in, as a rooted path. Default:
// the working directory, locally.
func WithFileViewDir(dir string) FileViewOption {
	return func(c *fileViewConfig) { c.dir = dir }
}

// WithFileViewStyles dresses every pane of a view.
func WithFileViewStyles(st FilePaneStyles) FileViewOption {
	return func(c *fileViewConfig) { c.st, c.styled = st, true }
}

// WithFileViewPreview sets whether a view shows a preview beside its listing.
// Default: on for an Open view, off for a Save view.
func WithFileViewPreview(v bool) FileViewOption {
	return func(c *fileViewConfig) { c.preview, c.previewSet = v, true }
}

// WithOnChoose sets what runs when a choice is made from inside the view:
// Enter on a file, or Enter in the name field with a name in it; and, for an
// async source, a typed name's check answering that it is the file (or, in a
// Folder view, the folder) a Confirm turned away while it was asked.
func WithOnChoose(fn func()) FileViewOption {
	return func(c *fileViewConfig) { c.onChoose = fn }
}

// WithFileViewFields puts fields at the top of a view's left column, above
// its own: a folder picker's name for what the folder becomes, say. Each is
// a part the keyboard moves to, as the view's own are.
func WithFileViewFields(fields ...tui.Component) FileViewOption {
	return func(c *fileViewConfig) { c.fields = append(c.fields, fields...) }
}

// WithOnHint sets what runs when the keyboard moves to another part of the
// dialog the view is in, with the keys that part answers to: a footer that
// says what Enter does HERE.
func WithOnHint(fn func(string)) FileViewOption {
	return func(c *fileViewConfig) { c.onHint = fn }
}

// ---------------------------------------------------------------- Open

// FileOpenView chooses an existing file: a FileList, and a FilePreview beside
// it unless turned off.
//
// `/` in the list opens a filter field above it, which narrows the rows as it is
// typed in; Enter goes back to the list with the filter kept, Esc clears it.
// The Save and Folder views' listings have no filter.
type FileOpenView struct {
	Base
	cfg     fileViewConfig
	list    *FileList
	preview *FilePreview // nil when turned off
	root    tui.Component
	// column is the left column's parts, top to bottom: the fields, then the
	// list. Ctrl+j/k move down and up it; Ctrl+l goes to the preview, and
	// Ctrl+h back to the part of the column last in use.
	column []tui.Component
	last   tui.Component
	// filterField narrows the listing as it is typed in; hidden until `/`.
	// nil when the view has no filter.
	filterField *TextInput
	filterPane  *Box
	// path is the location bar, just above the list; enterPath is what Enter on a path typed in
	// it does once it is known what the path names (later: found off the loop). The Save and
	// Folder views give their own; the Open view's lists a folder and chooses a file.
	path      *pathField
	enterPath func(p string, k pathKind, later bool)
	// chosen is a file chosen by its typed path, rooted, which Selected reports until the
	// cursor moves: the listing that puts the cursor on it may still be on its way.
	chosen string
}

// NewFileOpenView builds an Open view.
func NewFileOpenView(opts ...FileViewOption) *FileOpenView {
	cfg := fileViewConfigOf(opts)
	if !cfg.previewSet {
		cfg.preview = true
	}
	cfg.filter = true
	return newFileOpenView(cfg)
}

func newFileOpenView(cfg fileViewConfig, dirOpts ...FileListOption) *FileOpenView {
	// The source is settled HERE, before the list exists: the list reports its
	// first row while it is being built, and the preview reads that row from
	// the source.
	cfg.src = cfg.src.or()
	v := &FileOpenView{cfg: cfg}
	v.path = newPathField(cfg, openPathTitles, func(typed string) { v.goTyped(typed) })
	v.enterPath = v.openPath
	if cfg.preview {
		v.preview = NewFilePreview(cfg.st, cfg.styled)
	}
	listOpts := []FileListOption{
		WithFileListSource(cfg.src),
		WithFileListDir(cfg.dir),
		WithOnCursor(func(p string, folder bool) { v.chosen = ""; v.show(p, folder) }),
		WithOnFile(func(string) { v.chose() }),
	}
	if cfg.styled {
		listOpts = append(listOpts, WithFileListStyles(cfg.st))
	}
	v.list = NewFileList(append(listOpts, dirOpts...)...)
	// the location bar follows the listing, whatever else follows it
	onDir := v.list.onDir
	v.list.onDir = func(rooted string) {
		v.path.follow(rooted)
		if onDir != nil {
			onDir(rooted)
		}
	}
	above := append(append([]tui.Component(nil), cfg.fields...), v.path.pane)
	if cfg.filter {
		inputOpts := []TextInputOption{WithOnEdit(func(text string) { v.list.SetFilter(text) })}
		if cfg.styled {
			inputOpts = append(inputOpts, WithTextInputStyles(TextInputStyles{Text: cfg.st.Surface}))
		}
		v.filterField = NewTextInput(inputOpts...)
		v.filterPane = newFilePane(v.filterField, "", cfg.st, cfg.styled)
		v.filterPane.SetTitleMessage(tui.Msg("tui.files.filter"))
		v.filterPane.SetVisible(false)
		above = append(above, v.filterPane)
		// a new folder clears the filter, so the field goes with it
		v.list.onShow = func() { v.closeFilter() }
	}
	// The picker's layout: the fields (and the filter) over the list in the left
	// column, the preview on the right.
	v.column = append(append([]tui.Component(nil), cfg.fields...), v.path.pane, v.list)
	col := tui.NewFlex(tui.Vertical)
	for _, f := range above {
		col.Add(f)
	}
	col.AddWeighted(v.list, 1)
	var left tui.Component = col
	v.root = left
	if v.preview != nil {
		// A BLANK DIVIDER: the two panes are framed, and a drawn line between
		// two frames reads as a third.
		splitOpts := []SplitOption{WithRatio(0.45), WithSplitDividerGlyphs(" ", " ")}
		if cfg.styled {
			splitOpts = append(splitOpts, WithDividerStyle(cfg.st.Gap))
		}
		v.root = NewSplit(Horizontal, left, v.preview, splitOpts...)
	}
	return v
}

// movePart is Ctrl+h/j/k/l among the view's parts: down and up the left
// column, across to the preview and back. Reports whether it moved.
func (v *FileOpenView) movePart(k tui.KeyEvent) bool {
	if k.Mods != tui.ModCtrl {
		return false
	}
	ctx := v.Context()
	if ctx == nil {
		return false
	}
	at := -1
	for i, p := range v.column {
		if ctx.FocusWithin(p) {
			at = i
		}
	}
	switch k.Code {
	case 'j', 'k':
		if at < 0 {
			return false
		}
		next := at + 1
		if k.Code == 'k' {
			next = at - 1
		}
		if next < 0 || next >= len(v.column) {
			return true // the column's end: nothing below, or above
		}
		return ctx.FocusInto(v.column[next]) || true
	case 'l':
		if at < 0 || v.preview == nil {
			return false
		}
		v.last = v.column[at]
		return ctx.FocusInto(v.preview) || true
	case 'h':
		if at >= 0 || v.preview == nil || !ctx.FocusWithin(v.preview) {
			return false
		}
		back := v.last
		if back == nil {
			back = v.list
		}
		return ctx.FocusInto(back) || true
	}
	return false
}

// show previews a row, when there is a preview.
func (v *FileOpenView) show(p string, folder bool) {
	if v.preview != nil {
		v.preview.Show(v.cfg.src, p, folder)
	}
}

// chose runs the choice callback. The list only reports a FILE, so there is
// nothing to decide.
func (v *FileOpenView) chose() {
	if v.cfg.onChoose != nil {
		v.cfg.onChoose()
	}
}

func (v *FileOpenView) Init(ctx *tui.Context) {
	v.Base.Init(ctx)
	ctx.Mount(v.root)
}

// Layout takes most of what it is offered: a file dialog is a place to look
// through files, and a small one shows a handful of names.
func (v *FileOpenView) Layout(c tui.Constraints) tui.Size {
	return layoutFileView(v.Context(), v.root, c)
}

// layoutFileView is every file view's size: most of the offer, and a
// reasonable fixed size when there is no bound.
func layoutFileView(ctx *tui.Context, root tui.Component, c tui.Constraints) tui.Size {
	want := tui.Size{W: 72, H: 16}
	if c.MaxW != tui.Unbounded {
		want.W = max(min(c.MaxW, 40), c.MaxW*9/10)
	}
	if c.MaxH != tui.Unbounded {
		want.H = max(min(c.MaxH, 10), c.MaxH*3/4)
	}
	sz := c.Constrain(want)
	ctx.LayoutChild(root, tui.Tight(sz))
	ctx.PlaceChild(root, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (v *FileOpenView) Render(tui.Surface) {}

// HandleEvent follows the keyboard for the footer. A FocusEvent bubbles up from
// the part that gained or lost it, so the view hears every move among its
// parts and the move out to the dialog's buttons. Not consumed.
func (v *FileOpenView) HandleEvent(ev tui.Event) bool {
	if r, ok := ev.(tui.TaskResult); ok {
		return v.path.stat.answer(r, v.NodeID())
	}
	if _, ok := ev.(tui.FocusEvent); ok && v.cfg.onHint != nil {
		v.cfg.onHint(v.Hint())
	}
	if k, ok := ev.(tui.KeyEvent); ok {
		return v.pathKey(k) || v.filterKey(k) || v.movePart(k)
	}
	return false
}

// pathKey is the path field's keys, heard as they bubble up: ~ in the list opens the field with
// ~/ written, as a file chooser's location bar does; Esc in the field puts back the folder listed
// and returns to the list. Reports whether the key was the field's.
func (v *FileOpenView) pathKey(k tui.KeyEvent) bool {
	ctx := v.Context()
	if ctx == nil || k.Kind == tui.KeyRelease {
		return false
	}
	switch {
	case k.Text == "~" && k.Mods&nonTextMods == 0 && v.list.Focused():
		ctx.FocusInto(v.path.input)
		v.path.input.SetValue("~/")
		return true
	case k.Code == tui.KeyEscape && ctx.FocusWithin(v.path.pane):
		v.path.restore()
		v.list.Focus()
		return true
	}
	return false
}

// goTyped goes to the path typed in the field: once what it names is known, the view's enterPath
// decides what that means; a path that cannot be read as one of the source's says so.
func (v *FileOpenView) goTyped(typed string) {
	src, dir := v.list.Source(), v.list.Dir()
	p, bad, ok := v.path.resolve(src, dir, typed)
	if !ok {
		v.path.say(bad)
		return
	}
	still := func() bool {
		q, _, ok := v.path.resolve(v.list.Source(), v.list.Dir(), v.path.input.Value())
		return ok && q == p
	}
	v.path.check(v.Context(), src, p, still, func(k pathKind, later bool) { v.enterPath(p, k, later) })
}

// openPath is the Open view's Enter on a typed path: a folder is listed; a file is listed in its
// folder, the cursor on it, and chosen, as choosing it in the list would.
func (v *FileOpenView) openPath(p string, k pathKind, _ bool) {
	switch k {
	case pathFolder:
		v.list.load(p)
		v.list.Focus()
	case pathFile:
		v.list.loadThen(path.Dir(p), path.Base(p))
		v.chosen = v.list.Source().Rooted(p)
		v.chose()
	default:
		v.path.say(v.path.titleFor(k))
	}
}

// filterKey is the filter's keys, heard as they bubble up: `/` in the list
// opens the field; Enter in the field returns to the list, the filter kept; Esc
// clears the filter first, and only once it is clear goes on to close the
// dialog. Reports whether the key was the filter's.
func (v *FileOpenView) filterKey(k tui.KeyEvent) bool {
	ctx := v.Context()
	if v.filterField == nil || ctx == nil || k.Kind == tui.KeyRelease {
		return false
	}
	inField := ctx.FocusWithin(v.filterPane)
	switch {
	case k.Code == '/' && k.Mods == 0 && v.list.Focused():
		v.filterPane.SetVisible(true)
		v.filterField.SetValue(v.list.Filter())
		ctx.FocusInto(v.filterField)
		return true
	case k.Code == tui.KeyEnter && inField:
		if v.list.Filter() == "" {
			v.filterPane.SetVisible(false)
		}
		v.list.Focus()
		return true
	case k.Code == tui.KeyEscape && (inField || v.list.Filter() != ""):
		v.closeFilter()
		v.list.SetFilter("")
		v.list.Focus()
		return true
	}
	return false
}

// closeFilter empties and hides the filter's field. The listing's own filter
// is the caller's: a new folder has cleared it already.
func (v *FileOpenView) closeFilter() {
	if v.filterField == nil {
		return
	}
	v.filterField.SetValue("")
	v.filterPane.SetVisible(false)
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the view is not a tab stop; its
// list, preview and field are. Absence of the capability says so.

// Confirm chooses the file under the cursor, or goes into the folder there.
func (v *FileOpenView) Confirm() bool { return v.list.Enter() }

// Selected is the file under the cursor, rooted, or "" when it is a folder. A file chosen by its
// typed path is that file, until the cursor moves.
func (v *FileOpenView) Selected() string {
	if v.chosen != "" {
		return v.chosen
	}
	p, folder, ok := v.list.Current()
	if !ok || folder {
		return ""
	}
	return v.list.Source().Rooted(p)
}

// Select lists the file's folder with the cursor on it.
func (v *FileOpenView) Select(rooted string) {
	p, ok := v.cfg.src.fsPath(rooted)
	if !ok {
		return
	}
	// the cursor goes to the file once its folder is listed, which for a VFS
	// source is later; a newer load supersedes it
	v.list.loadThen(path.Dir(p), path.Base(p))
}

// SetSource lists another filesystem (FileList.SetSource), the preview reading
// from it too.
func (v *FileOpenView) SetSource(src FileSource) {
	v.path.stat.drop() // a check, or its answer, is the old source's
	v.chosen = ""
	v.cfg.src = src.or()
	v.list.SetSource(v.cfg.src)
}

func (v *FileOpenView) Dir() string          { return v.list.Source().Rooted(v.list.Dir()) }
func (v *FileOpenView) SetDir(rooted string) { v.list.SetDir(rooted) }
func (v *FileOpenView) FocusInitial()        { v.list.Focus() }

// SetPreviewHighlighting highlights the preview: see
// [FilePreview.SetHighlighting]. A view with no preview ignores it.
func (v *FileOpenView) SetPreviewHighlighting(forFile func(name string) highlight.Highlighter, styles SyntaxStyles) {
	if v.preview != nil {
		v.preview.SetHighlighting(forFile, styles)
	}
}

// SetPreviewSourceHighlighting installs the immutable source catalog on the preview.
func (v *FileOpenView) SetPreviewSourceHighlighting(catalog *highlight.Catalog, styles SyntaxStyles) {
	if v.preview != nil {
		v.preview.SetSourceHighlighting(catalog, styles)
	}
}

// PreviewText is what the preview holds, "" without one.
func (v *FileOpenView) PreviewText() string {
	if v.preview == nil {
		return ""
	}
	return v.preview.Text()
}

// Hint is the keys the part with the keyboard answers to, in the App's language.
func (v *FileOpenView) Hint() string { return v.translate(v.HintMessage()) }

// HintMessage is [FileOpenView.Hint] as a catalog message, for a footer that follows the
// language by itself. Each footer is one whole message, never assembled from translated
// parts, so every language can order it as it reads.
func (v *FileOpenView) HintMessage() tui.Message {
	switch {
	case v.filterField != nil && v.filterField.Context() != nil && v.filterField.Context().Focused():
		return tui.Msg("tui.files.hint.filter")
	case v.path.focused():
		return tui.Msg("tui.files.hint.path")
	case v.list.Focused():
		return tui.Msg("tui.files.hint.openList")
	case v.preview != nil && v.preview.Focused():
		return tui.Msg("tui.files.hint.previewPart")
	}
	return tui.Msg("tui.files.hint.default")
}

// ---------------------------------------------------------------- Save

// FileSaveView names a file to write: a FileOpenView with a name field at the
// top of its left column.
type FileSaveView struct {
	Base
	cfg      fileViewConfig
	name     *TextInput
	namePane *Box
	listing  *FileOpenView
	root     tui.Component
	stat     typedStat
}

// NewFileSaveView builds a Save view. Its listing has no preview unless asked.
func NewFileSaveView(opts ...FileViewOption) *FileSaveView {
	cfg := fileViewConfigOf(opts)
	v := &FileSaveView{cfg: cfg}

	inputOpts := []TextInputOption{WithOnSubmit(func(string) { v.confirm() }), WithOnEdit(func(string) { v.stat.drop() })}
	if cfg.styled {
		inputOpts = append(inputOpts, WithTextInputStyles(TextInputStyles{Text: cfg.st.Surface}))
	}
	v.name = NewTextInput(inputOpts...)
	v.namePane = newFilePane(v.name, "", cfg.st, cfg.styled)
	v.namePane.SetTitleMessage(tui.Msg("tui.files.fileName"))

	inner := cfg
	inner.onHint = nil // the Save view reports the footer for all its parts
	inner.fields = append(append([]tui.Component(nil), cfg.fields...), v.namePane)
	// A file chosen in the listing is NAMED, then the name is confirmed — the
	// one path a save takes, whether the name was typed or picked.
	inner.onChoose = func() { v.SetName(path.Base(v.listing.Selected())); v.confirm() }
	v.listing = newFileOpenView(inner)
	// Moving onto a file in the listing names it, as picking one does.
	v.listing.list.onCursor = func(p string, folder bool) {
		v.listing.show(p, folder)
		if !folder {
			v.SetName(path.Base(p))
		}
	}
	v.listing.enterPath = v.savePath
	v.root = v.listing
	return v
}

// savePath is the Save view's Enter on a typed path. A folder is listed, the
// name kept. A file — or a name not there yet, in a folder that is — is the
// target: its folder listed and the name filled, the keyboard on the name. A
// path's Enter never saves; Enter on the name does, as for a typed name. A
// folder that does not exist is refused, never made.
func (v *FileSaveView) savePath(p string, k pathKind, _ bool) {
	f, list := v.listing.path, v.listing.list
	switch k {
	case pathFolder:
		list.load(p)
		v.focusName()
	case pathFile:
		v.target(p)
	case pathMissing:
		dir := path.Dir(p)
		if !list.Loading() && dir == list.Dir() {
			v.target(p) // a new name in the folder listed
			return
		}
		still := func() bool {
			q, _, ok := f.resolve(list.Source(), list.Dir(), f.input.Value())
			return ok && q == p
		}
		f.check(v.listing.Context(), list.Source(), dir, still, func(k pathKind, _ bool) {
			if k == pathFolder {
				v.target(p)
			} else {
				f.say(f.titles.noFolder)
			}
		})
	default:
		f.say(f.titleFor(k))
	}
}

// target names p, in its folder: listed, unless it is already, with the cursor
// on p when it is there; then the name, and the keyboard on it.
func (v *FileSaveView) target(p string) {
	list := v.listing.list
	if dir := path.Dir(p); list.Loading() || dir != list.Dir() {
		list.loadThen(dir, path.Base(p))
	} else {
		list.SetCurrent(path.Base(p))
	}
	v.SetName(path.Base(p))
	v.focusName()
}

// focusName gives the keyboard to the name field.
func (v *FileSaveView) focusName() {
	if ctx := v.Context(); ctx != nil {
		ctx.FocusInto(v.name)
	}
}

func (v *FileSaveView) confirm() {
	if v.Confirm() && v.cfg.onChoose != nil {
		v.cfg.onChoose()
	}
}

func (v *FileSaveView) Init(ctx *tui.Context) {
	v.Base.Init(ctx)
	ctx.Mount(v.root)
}

func (v *FileSaveView) Layout(c tui.Constraints) tui.Size {
	return layoutFileView(v.Context(), v.root, c)
}

func (v *FileSaveView) Render(tui.Surface) {}

// HandleEvent applies a typed name's folder check when it arrives, and follows
// the keyboard for the footer, as FileOpenView's does.
func (v *FileSaveView) HandleEvent(ev tui.Event) bool {
	if r, ok := ev.(tui.TaskResult); ok {
		return v.stat.answer(r, v.NodeID())
	}
	if _, ok := ev.(tui.FocusEvent); ok && v.cfg.onHint != nil {
		v.cfg.onHint(v.Hint())
	}
	return false
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable, as FileOpenView.

// Confirm chooses the named file — unless the name is a folder, which it goes
// into, clearing the name, rather than writing over.
//
// For an async source the name is checked off the loop: Confirm reports false,
// and once the answer is in, a folder is gone into and a file chosen through
// WithOnChoose, whose Confirm then reports true. An edit to the name, or a
// newer folder, drops a check still on its way.
func (v *FileSaveView) Confirm() bool {
	p, ok := v.named()
	if !ok {
		return false
	}
	src := v.listing.list.Source()
	k, known := v.stat.known(p)
	folder := k == pathFolder
	ctx := v.Context()
	if !known && src.async() && ctx != nil {
		v.stat.ask(ctx, src, p, func(k pathKind) {
			if q, ok := v.named(); !ok || q != p {
				return // the name or its folder changed under the check
			}
			if k == pathFolder {
				v.into(p)
			} else if v.cfg.onChoose != nil {
				v.cfg.onChoose()
			}
		})
		return false
	}
	if !known {
		folder = src.isDir(context.Background(), p)
	}
	if folder {
		v.into(p)
		return false
	}
	return true
}

// into goes into the folder the name named, clearing the name.
func (v *FileSaveView) into(p string) {
	v.stat.drop()
	v.name.SetValue("")
	v.listing.list.load(p)
}

// named is the name field as an fs path in the listed folder, or false when
// it is empty or names nothing inside the source.
func (v *FileSaveView) named() (string, bool) {
	name := strings.TrimSpace(v.name.Value())
	if name == "" {
		return "", false
	}
	src := v.listing.list.Source()
	if strings.HasPrefix(name, src.Root) || strings.HasPrefix(name, "/") {
		return src.fsPath(name)
	}
	p := path.Clean(path.Join(v.listing.list.Dir(), name))
	return p, fs.ValidPath(p)
}

// Selected is the named file, rooted, or "" when no name is written.
func (v *FileSaveView) Selected() string {
	p, ok := v.named()
	if !ok {
		return ""
	}
	return v.listing.list.Source().Rooted(p)
}

// Select lists the file's folder and names the file — whether it exists yet
// or not, which is the point of a name field.
func (v *FileSaveView) Select(rooted string) {
	v.stat.drop()
	v.listing.Select(rooted)
	if p, ok := v.listing.cfg.src.fsPath(rooted); ok && p != "." {
		v.name.SetValue(path.Base(p))
	}
}

func (v *FileSaveView) Dir() string { return v.listing.Dir() }
func (v *FileSaveView) SetDir(rooted string) {
	v.stat.drop()
	v.listing.SetDir(rooted)
}

// SetSource lists another filesystem (FileOpenView.SetSource).
func (v *FileSaveView) SetSource(src FileSource) {
	v.stat.drop() // a check, or its answer, is the old source's
	v.cfg.src = src.or()
	v.listing.SetSource(v.cfg.src)
}

// FocusInitial gives the keyboard to the name field: a user saving came to
// type a name.
func (v *FileSaveView) FocusInitial() {
	if ctx := v.Context(); ctx != nil {
		ctx.FocusComponent(v.name)
	}
}

// Name is what the name field holds.
func (v *FileSaveView) Name() string { return v.name.Value() }

// SetName puts s in the name field; "" empties it.
func (v *FileSaveView) SetName(s string) {
	v.stat.drop()
	v.name.SetValue(s)
}

// SetPreviewHighlighting highlights the preview, when it has one: see
// [FileOpenView].
func (v *FileSaveView) SetPreviewHighlighting(forFile func(name string) highlight.Highlighter, styles SyntaxStyles) {
	v.listing.SetPreviewHighlighting(forFile, styles)
}

// SetPreviewSourceHighlighting selects complete source behavior for the listing preview.
func (v *FileSaveView) SetPreviewSourceHighlighting(catalog *highlight.Catalog, styles SyntaxStyles) {
	v.listing.SetPreviewSourceHighlighting(catalog, styles)
}

// Hint is the keys the part with the keyboard answers to, in the App's language.
func (v *FileSaveView) Hint() string { return v.translate(v.HintMessage()) }

// HintMessage is [FileSaveView.Hint] as a catalog message, as [FileOpenView.HintMessage].
func (v *FileSaveView) HintMessage() tui.Message {
	if ctx := v.name.Context(); ctx != nil && ctx.Focused() {
		return tui.Msg("tui.files.hint.saveName")
	}
	if v.listing.list.Focused() {
		return tui.Msg("tui.files.hint.saveList")
	}
	return v.listing.HintMessage()
}

// typedStat is a typed path's check, for an async source: what it names, found
// off the loop, its answer applied once it is in, unless a drop came first. The
// last answer is kept, for the Confirm that follows it, until the next drop.
type typedStat struct {
	task    tui.TaskID
	stop    context.CancelFunc
	pending bool
	asked   string
	then    func(k pathKind)
	// has says last, and what it names, is a check's answer
	has  bool
	last string
	kind pathKind
}

// ask checks p off the loop, in place of any check on its way.
func (s *typedStat) ask(ctx *tui.Context, src FileSource, p string, then func(k pathKind)) {
	s.drop()
	s.pending, s.asked, s.then = true, p, then
	s.task, s.stop = goRead(ctx, "files.stat", func(c context.Context) (any, error) {
		return src.kind(c, p), nil
	})
}

// known is a check's answer for p, when there is one.
func (s *typedStat) known(p string) (k pathKind, ok bool) { return s.kind, s.has && s.last == p }

// drop stops the check on its way, if any, and forgets the last answer.
func (s *typedStat) drop() {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	s.pending, s.then, s.has = false, nil, false
}

// answer applies the check's result when r is it, and reports whether r was
// the owner's.
func (s *typedStat) answer(r tui.TaskResult, owner tui.NodeID) bool {
	if r.ID != s.task || !s.pending {
		return r.Owner == owner
	}
	k, _ := r.Value.(pathKind)
	then := s.then
	s.drop()
	s.has, s.last, s.kind = true, s.asked, k
	then(k)
	return true
}

// ---------------------------------------------------------------- Folder

// FileFolderView chooses a folder: a FileOpenView whose path field, titled
// Folder, is the answer. The field FOLLOWS the listing — moving into a folder
// writes it there — and a path typed in it, then Enter, lists that folder. The
// folder chosen is the field's. Files are listed too, and previewed, so a
// folder is seen for what is in it.
type FileFolderView struct {
	Base
	cfg     fileViewConfig
	listing *FileOpenView
}

// NewFileFolderView builds a folder view. Its listing previews unless asked
// not to.
func NewFileFolderView(opts ...FileViewOption) *FileFolderView {
	cfg := fileViewConfigOf(opts)
	if !cfg.previewSet {
		cfg.preview = true
	}
	v := &FileFolderView{cfg: cfg}
	inner := cfg
	inner.onHint = nil // the folder view reports the footer for all its parts
	inner.onChoose = nil
	v.listing = newFileOpenView(inner)
	v.listing.path.titles = folderPathTitles
	v.listing.path.pane.SetTitleMessage(folderTitle)
	v.listing.enterPath = func(p string, k pathKind, _ bool) {
		if k == pathFolder {
			v.listing.list.load(p)
			return
		}
		v.listing.path.say(v.listing.path.titleFor(k))
	}
	return v
}

// The folder field's titles: the field, and the field holding a path that is not a folder.
var (
	folderTitle    = tui.Msg("tui.files.folder")
	notFolderTitle = tui.Msg("tui.files.folderNotAFolder")
)

// field is the path field the view's answer is in.
func (v *FileFolderView) field() *pathField { return v.listing.path }

// checkThen runs then with typed as an fs path when it is a folder, and says
// what it is instead in the field's title when it is not. A path not known to
// be a folder, of an async source, is checked off the loop, and then told it
// ran later; an edit to the field, or the field following the listing
// elsewhere, drops the check.
func (v *FileFolderView) checkThen(typed string, then func(p string, later bool)) {
	f, list := v.field(), v.listing.list
	p, bad, ok := f.resolve(list.Source(), list.Dir(), typed)
	if !ok {
		f.say(bad)
		return
	}
	if !list.Loading() && p == list.Dir() {
		then(p, false) // the folder listed is one
		return
	}
	still := func() bool {
		q, _, ok := f.resolve(list.Source(), list.Dir(), f.input.Value())
		return ok && q == p
	}
	f.check(v.listing.Context(), list.Source(), p, still, func(k pathKind, later bool) {
		if k == pathFolder {
			then(p, later)
		} else {
			f.say(f.titleFor(k))
		}
	})
}

// folder is typed as an fs path of the source, when it is known to be a folder
// there without a read of an async source: the folder listed is one, and so is
// a path a check found to be. Of a source read on the loop, it asks the source.
func (v *FileFolderView) folder(typed string) (string, bool) {
	f, list := v.field(), v.listing.list
	p, _, ok := f.resolve(list.Source(), list.Dir(), typed)
	if !ok {
		return "", false
	}
	src := list.Source()
	if !src.async() {
		return p, src.isDir(context.Background(), p)
	}
	if !list.Loading() && p == list.Dir() {
		return p, true
	}
	k, known := f.stat.known(p)
	return p, known && k == pathFolder
}

func (v *FileFolderView) Init(ctx *tui.Context) {
	v.Base.Init(ctx)
	ctx.Mount(v.listing)
}

func (v *FileFolderView) Layout(c tui.Constraints) tui.Size {
	return layoutFileView(v.Context(), v.listing, c)
}

func (v *FileFolderView) Render(tui.Surface) {}

// HandleEvent follows the keyboard for the footer, as FileOpenView's does; the
// path field's check is the listing's.
func (v *FileFolderView) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.FocusEvent); ok && v.cfg.onHint != nil {
		v.cfg.onHint(v.Hint())
	}
	return false
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable, as FileOpenView.

// Confirm chooses the folder in the path field, when it is one. A path that
// is not says so, and nothing is chosen.
//
// For an async source a path not known to be a folder is checked off the loop:
// Confirm reports false, and a folder is then chosen through WithOnChoose,
// whose Confirm reports true.
func (v *FileFolderView) Confirm() bool {
	now := false
	v.checkThen(v.field().input.Value(), func(_ string, later bool) {
		if !later {
			now = true
		} else if v.cfg.onChoose != nil {
			v.cfg.onChoose()
		}
	})
	return now
}

// Selected is the folder in the path field, rooted, or "" when it is not one —
// or, of an async source, not known to be one without a read.
func (v *FileFolderView) Selected() string {
	p, ok := v.folder(v.field().input.Value())
	if !ok {
		return ""
	}
	return v.listing.list.Source().Rooted(p)
}

// Select lists a folder, which the path field then holds.
func (v *FileFolderView) Select(rooted string) { v.SetDir(rooted) }

func (v *FileFolderView) Dir() string { return v.listing.Dir() }
func (v *FileFolderView) SetDir(rooted string) {
	v.field().stat.drop()
	v.listing.SetDir(rooted)
}

// SetSource lists another filesystem (FileOpenView.SetSource); the path field
// follows the listing to it.
func (v *FileFolderView) SetSource(src FileSource) {
	v.cfg.src = src.or()
	v.listing.SetSource(v.cfg.src)
}

// FocusInitial gives the keyboard to the first field: the host's own, when it
// gave one (a name to type), else the listing. A dialog opening in this very
// turn has not mounted the field yet; its card then gives the keyboard to the
// first thing in it that takes it, which is the same field — so the listing
// must not take it here.
func (v *FileFolderView) FocusInitial() {
	ctx := v.Context()
	if ctx == nil {
		return
	}
	if len(v.cfg.fields) > 0 {
		ctx.FocusInto(v.cfg.fields[0])
		return
	}
	v.listing.list.Focus()
}

// Path is what the path field holds.
func (v *FileFolderView) Path() string { return v.field().input.Value() }

// SetPreviewHighlighting highlights the preview: see [FileOpenView].
func (v *FileFolderView) SetPreviewHighlighting(forFile func(name string) highlight.Highlighter, styles SyntaxStyles) {
	v.listing.SetPreviewHighlighting(forFile, styles)
}

// SetPreviewSourceHighlighting selects complete source behavior for the folder preview.
func (v *FileFolderView) SetPreviewSourceHighlighting(catalog *highlight.Catalog, styles SyntaxStyles) {
	v.listing.SetPreviewSourceHighlighting(catalog, styles)
}

// Hint is the keys the part with the keyboard answers to, in the App's language.
func (v *FileFolderView) Hint() string { return v.translate(v.HintMessage()) }

// HintMessage is [FileFolderView.Hint] as a catalog message, as [FileOpenView.HintMessage].
func (v *FileFolderView) HintMessage() tui.Message {
	if v.field().focused() {
		return tui.Msg("tui.files.hint.folderPath")
	}
	if v.listing.list.Focused() {
		return tui.Msg("tui.files.hint.listPart")
	}
	return v.listing.HintMessage()
}

var _ FileChooser = (*FileFolderView)(nil)
