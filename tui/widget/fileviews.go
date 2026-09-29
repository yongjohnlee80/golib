package widget

import (
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
// Enter on a file, or Enter in the name field with a name in it.
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

// hintFor is the footer for a part of a view: that part's keys, then the ones
// every part shares. Written once, so every file view's footer reads alike.
func hintFor(part string) string {
	if part == "" {
		return "Enter:press  Tab:next  Esc:cancel"
	}
	return part + "  Tab:next  Esc:cancel"
}

// ---------------------------------------------------------------- Open

// FileOpenView chooses an existing file: a FileList, and a FilePreview beside
// it unless turned off.
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
}

// NewFileOpenView builds an Open view.
func NewFileOpenView(opts ...FileViewOption) *FileOpenView {
	cfg := fileViewConfigOf(opts)
	if !cfg.previewSet {
		cfg.preview = true
	}
	return newFileOpenView(cfg)
}

func newFileOpenView(cfg fileViewConfig, dirOpts ...FileListOption) *FileOpenView {
	// The source is settled HERE, before the list exists: the list reports its
	// first row while it is being built, and the preview reads that row from
	// the source.
	cfg.src = cfg.src.or()
	v := &FileOpenView{cfg: cfg}
	if cfg.preview {
		v.preview = NewFilePreview(cfg.st, cfg.styled)
	}
	listOpts := []FileListOption{
		WithFileListSource(cfg.src),
		WithFileListDir(cfg.dir),
		WithOnCursor(func(p string, folder bool) { v.show(p, folder) }),
		WithOnFile(func(string) { v.chose() }),
	}
	if cfg.styled {
		listOpts = append(listOpts, WithFileListStyles(cfg.st))
	}
	v.list = NewFileList(append(listOpts, dirOpts...)...)
	// The picker's layout: the fields over the list in the left column, the
	// preview on the right.
	v.column = append(append([]tui.Component(nil), cfg.fields...), v.list)
	var left tui.Component = v.list
	if len(cfg.fields) > 0 {
		col := tui.NewFlex(tui.Vertical)
		for _, f := range cfg.fields {
			col.Add(f)
		}
		col.AddWeighted(v.list, 1)
		left = col
	}
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
	if _, ok := ev.(tui.FocusEvent); ok && v.cfg.onHint != nil {
		v.cfg.onHint(v.Hint())
	}
	if k, ok := ev.(tui.KeyEvent); ok {
		return v.movePart(k)
	}
	return false
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the view is not a tab stop; its
// list, preview and field are. Absence of the capability says so.

// Confirm chooses the file under the cursor, or goes into the folder there.
func (v *FileOpenView) Confirm() bool { return v.list.Enter() }

// Selected is the file under the cursor, rooted, or "" when it is a folder.
func (v *FileOpenView) Selected() string {
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
	v.list.load(path.Dir(p))
	v.list.SetCurrent(path.Base(p))
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

// PreviewText is what the preview holds, "" without one.
func (v *FileOpenView) PreviewText() string {
	if v.preview == nil {
		return ""
	}
	return v.preview.Text()
}

// Hint is the keys the part with the keyboard answers to.
func (v *FileOpenView) Hint() string {
	switch {
	case v.list.Focused():
		return hintFor(v.list.Hint() + ", or open file")
	case v.preview != nil && v.preview.Focused():
		return hintFor(v.preview.Hint())
	}
	return hintFor("")
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
}

// NewFileSaveView builds a Save view. Its listing has no preview unless asked.
func NewFileSaveView(opts ...FileViewOption) *FileSaveView {
	cfg := fileViewConfigOf(opts)
	v := &FileSaveView{cfg: cfg}

	inputOpts := []TextInputOption{WithOnSubmit(func(string) { v.confirm() })}
	if cfg.styled {
		inputOpts = append(inputOpts, WithTextInputStyles(TextInputStyles{Text: cfg.st.Surface}))
	}
	v.name = NewTextInput(inputOpts...)
	v.namePane = newFilePane(v.name, "File name", cfg.st, cfg.styled)

	inner := cfg
	inner.onHint = nil // the Save view reports the footer for all its parts
	inner.fields = append(append([]tui.Component(nil), cfg.fields...), v.namePane)
	// A file chosen in the listing is NAMED, then the name is confirmed — the
	// one path a save takes, whether the name was typed or picked.
	inner.onChoose = func() { v.name.SetValue(path.Base(v.listing.Selected())); v.confirm() }
	v.listing = newFileOpenView(inner)
	// Moving onto a file in the listing names it, as picking one does.
	v.listing.list.onCursor = func(p string, folder bool) {
		v.listing.show(p, folder)
		if !folder {
			v.name.SetValue(path.Base(p))
		}
	}
	v.root = v.listing
	return v
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

// HandleEvent follows the keyboard for the footer, as FileOpenView's does.
func (v *FileSaveView) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.FocusEvent); ok && v.cfg.onHint != nil {
		v.cfg.onHint(v.Hint())
	}
	return false
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable, as FileOpenView.

// Confirm chooses the named file — unless the name is a folder, which it goes
// into, clearing the name, rather than writing over.
func (v *FileSaveView) Confirm() bool {
	p, ok := v.named()
	if !ok {
		return false
	}
	src := v.listing.list.Source()
	if src.isDir(p) {
		v.name.SetValue("")
		v.listing.list.load(p)
		return false
	}
	return true
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
	v.listing.Select(rooted)
	if p, ok := v.listing.cfg.src.fsPath(rooted); ok && p != "." {
		v.name.SetValue(path.Base(p))
	}
}

func (v *FileSaveView) Dir() string          { return v.listing.Dir() }
func (v *FileSaveView) SetDir(rooted string) { v.listing.SetDir(rooted) }

// FocusInitial gives the keyboard to the name field: a user saving came to
// type a name.
func (v *FileSaveView) FocusInitial() {
	if ctx := v.Context(); ctx != nil {
		ctx.FocusComponent(v.name)
	}
}

// Name is what the name field holds.
func (v *FileSaveView) Name() string { return v.name.Value() }

// SetPreviewHighlighting highlights the preview, when it has one: see
// [FileOpenView].
func (v *FileSaveView) SetPreviewHighlighting(forFile func(name string) highlight.Highlighter, styles SyntaxStyles) {
	v.listing.SetPreviewHighlighting(forFile, styles)
}

// Hint is the keys the part with the keyboard answers to.
func (v *FileSaveView) Hint() string {
	if ctx := v.name.Context(); ctx != nil && ctx.Focused() {
		return hintFor("Enter:save")
	}
	if v.listing.list.Focused() {
		return hintFor(v.listing.list.Hint() + ", or save over file")
	}
	return v.listing.Hint()
}

// ---------------------------------------------------------------- Folder

// FileFolderView chooses a folder: a FileOpenView with a path field at the
// top of its left column. The field FOLLOWS the listing — moving into a folder
// writes it there — and a path typed in it, then Enter, lists that folder. The
// folder chosen is the field's. Files are listed too, and previewed, so a
// folder is seen for what is in it.
type FileFolderView struct {
	Base
	cfg      fileViewConfig
	path     *TextInput
	pathPane *Box
	listing  *FileOpenView
	// focusField is FocusInitial asked for the first field before it was
	// mounted — a dialog opening in this very turn — so the next layout gives
	// it the keyboard.
	focusField bool
}

// NewFileFolderView builds a folder view. Its listing previews unless asked
// not to.
func NewFileFolderView(opts ...FileViewOption) *FileFolderView {
	cfg := fileViewConfigOf(opts)
	if !cfg.previewSet {
		cfg.preview = true
	}
	v := &FileFolderView{cfg: cfg}
	inputOpts := []TextInputOption{WithOnSubmit(func(s string) { v.goTo(s) })}
	if cfg.styled {
		inputOpts = append(inputOpts, WithTextInputStyles(TextInputStyles{Text: cfg.st.Surface}))
	}
	v.path = NewTextInput(inputOpts...)
	v.pathPane = newFilePane(v.path, folderTitle, cfg.st, cfg.styled)

	inner := cfg
	inner.onHint = nil // the folder view reports the footer for all its parts
	inner.onChoose = nil
	inner.fields = append(append([]tui.Component(nil), cfg.fields...), v.pathPane)
	v.listing = newFileOpenView(inner, WithOnDir(func(dir string) {
		v.path.SetValue(dir)
		v.pathPane.SetTitle(folderTitle)
	}))
	return v
}

const folderTitle = "Folder"

// goTo lists the folder typed in the path field; a path that is not one says
// so in the field's title, and the listing stays.
func (v *FileFolderView) goTo(typed string) {
	if p, ok := v.folder(typed); ok {
		v.listing.list.load(p)
		return
	}
	v.pathPane.SetTitle(folderTitle + " — not a folder")
}

// folder is typed as an fs path of the source, when it is a folder there.
func (v *FileFolderView) folder(typed string) (string, bool) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return "", false
	}
	src := v.listing.list.Source()
	var p string
	var ok bool
	if strings.HasPrefix(typed, src.Root) || strings.HasPrefix(typed, "/") {
		p, ok = src.fsPath(typed)
	} else {
		p = path.Clean(path.Join(v.listing.list.Dir(), typed))
		ok = fs.ValidPath(p)
	}
	return p, ok && src.isDir(p)
}

func (v *FileFolderView) Init(ctx *tui.Context) {
	v.Base.Init(ctx)
	ctx.Mount(v.listing)
}

func (v *FileFolderView) Layout(c tui.Constraints) tui.Size {
	ctx := v.Context()
	sz := layoutFileView(ctx, v.listing, c)
	if v.focusField {
		v.focusField = false
		ctx.AfterLayout(folderFocusKey, func() { ctx.FocusInto(v.cfg.fields[0]) })
	}
	return sz
}

const folderFocusKey tui.CommitKey = "folder.focus-field"

func (v *FileFolderView) Render(tui.Surface) {}

// HandleEvent follows the keyboard for the footer, as FileOpenView's does.
func (v *FileFolderView) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.FocusEvent); ok && v.cfg.onHint != nil {
		v.cfg.onHint(v.Hint())
	}
	return false
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable, as FileOpenView.

// Confirm chooses the folder in the path field, when it is one. A path that
// is not says so, and nothing is chosen.
func (v *FileFolderView) Confirm() bool {
	if _, ok := v.folder(v.path.Value()); ok {
		return true
	}
	v.pathPane.SetTitle(folderTitle + " — not a folder")
	return false
}

// Selected is the folder in the path field, rooted, or "" when it is not one.
func (v *FileFolderView) Selected() string {
	p, ok := v.folder(v.path.Value())
	if !ok {
		return ""
	}
	return v.listing.list.Source().Rooted(p)
}

// Select lists a folder, which the path field then holds.
func (v *FileFolderView) Select(rooted string) { v.listing.SetDir(rooted) }

func (v *FileFolderView) Dir() string          { return v.listing.Dir() }
func (v *FileFolderView) SetDir(rooted string) { v.listing.SetDir(rooted) }

// FocusInitial gives the keyboard to the first field: the host's own, when it
// gave one (a name to type), else the listing.
func (v *FileFolderView) FocusInitial() {
	ctx := v.Context()
	if ctx == nil {
		return
	}
	if len(v.cfg.fields) > 0 && !ctx.FocusInto(v.cfg.fields[0]) {
		v.focusField = true // not mounted yet: the next layout focuses it
		ctx.RequestLayout()
		return
	}
	if len(v.cfg.fields) == 0 {
		v.listing.list.Focus()
	}
}

// Path is what the path field holds.
func (v *FileFolderView) Path() string { return v.path.Value() }

// SetPreviewHighlighting highlights the preview: see [FileOpenView].
func (v *FileFolderView) SetPreviewHighlighting(forFile func(name string) highlight.Highlighter, styles SyntaxStyles) {
	v.listing.SetPreviewHighlighting(forFile, styles)
}

// Hint is the keys the part with the keyboard answers to.
func (v *FileFolderView) Hint() string {
	if ctx := v.path.Context(); ctx != nil && ctx.Focused() {
		return hintFor("Enter:go to folder")
	}
	if v.listing.list.Focused() {
		return hintFor(v.listing.list.Hint())
	}
	return v.listing.Hint()
}

var _ FileChooser = (*FileFolderView)(nil)
