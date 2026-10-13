package widget

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// THE PATH FIELD — the file views' location bar: a field above the listing that holds the folder
// listed and follows it, and takes a typed path. Enter resolves what was typed (absolute, ~ or
// relative to the folder listed), finds out what it names (off the loop for an async source) and
// hands that to the view, which decides what going there means: the Open view chooses a file,
// the Save view names one, the Folder view lists a folder. Esc puts back the folder listed.

// pathKind is what a path names in a source.
type pathKind int

const (
	pathMissing    pathKind = iota // nothing there
	pathFile                       // a file (a link followed, for an fs.FS)
	pathFolder                     // a folder (a link to one, for an fs.FS)
	pathUnreadable                 // there, perhaps, but the source refused to say (permission)
)

// kind reports what an fs path names, following a link in an fs.FS, with ctx bounding a VFS read.
func (s FileSource) kind(ctx context.Context, p string) pathKind {
	var fi fs.FileInfo
	var err error
	if s.VFS != nil {
		var vi interface{ IsDir() bool }
		vi, err = s.VFS.Stat(ctx, p)
		if err == nil {
			return kindOf(vi.IsDir())
		}
	} else {
		fi, err = fs.Stat(s.FS, p)
		if err == nil {
			return kindOf(fi.IsDir())
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return pathMissing
	}
	return pathUnreadable
}

func kindOf(folder bool) pathKind {
	if folder {
		return pathFolder
	}
	return pathFile
}

// pathTitles are a path field's titles: the field's own, and the field holding a path that is
// not found, names no folder its view can use, has no folder to go in, uses a ~ the source does
// not know, or cannot be read. Each is one whole message, so every language words it whole.
type pathTitles struct {
	field, notFound, notFolder, noFolder, noHome, cannotRead tui.Message
}

var (
	openPathTitles = pathTitles{
		field: tui.Msg("tui.files.path"), notFound: tui.Msg("tui.files.pathNotFound"),
		notFolder: tui.Msg("tui.files.pathNotFound"), noFolder: tui.Msg("tui.files.pathNoFolder"),
		noHome: tui.Msg("tui.files.pathNoHome"), cannotRead: tui.Msg("tui.files.pathCannotRead"),
	}
	folderPathTitles = pathTitles{
		field: folderTitle, notFound: tui.Msg("tui.files.folderNotFound"),
		notFolder: notFolderTitle, noFolder: tui.Msg("tui.files.folderNotFound"),
		noHome: tui.Msg("tui.files.folderNoHome"), cannotRead: tui.Msg("tui.files.folderCannotRead"),
	}
)

// pathField is the field, its pane and its check: the part the three views share.
type pathField struct {
	input  *TextInput
	pane   *Box
	titles pathTitles
	stat   typedStat
	// shown is the folder listed, rooted, as the field last followed it: what Esc puts back.
	shown string
}

func newPathField(cfg fileViewConfig, titles pathTitles, submit func(string)) *pathField {
	f := &pathField{titles: titles}
	inputOpts := []TextInputOption{WithOnSubmit(submit), WithOnEdit(func(string) {
		f.stat.drop()
		f.pane.SetTitleMessage(f.titles.field)
	})}
	if cfg.styled {
		inputOpts = append(inputOpts, WithTextInputStyles(TextInputStyles{Text: cfg.st.Surface}))
	}
	f.input = NewTextInput(inputOpts...)
	f.pane = newFilePane(f.input, "", cfg.st, cfg.styled)
	f.pane.SetTitleMessage(titles.field)
	return f
}

// follow writes the folder listed into the field, dropping a check of what was typed before.
func (f *pathField) follow(rooted string) {
	f.stat.drop()
	f.shown = rooted
	f.input.SetValue(rooted)
	f.pane.SetTitleMessage(f.titles.field)
}

// restore puts back the folder listed, as Esc does.
func (f *pathField) restore() { f.follow(f.shown) }

// say shows m as the field's title: what was typed could not be gone to.
func (f *pathField) say(m tui.Message) { f.pane.SetTitleMessage(m) }

// focused reports whether the field has the keyboard.
func (f *pathField) focused() bool {
	ctx := f.input.Context()
	return ctx != nil && ctx.Focused()
}

// resolve reads typed as an fs path of src: ~ and ~/x against its Home, a path starting with its
// Root or / as rooted, anything else relative to dir, the folder listed. bad is the title to show
// when it cannot be read as one: ~ in a source without a Home, or a path outside the source.
func (f *pathField) resolve(src FileSource, dir, typed string) (p string, bad tui.Message, ok bool) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return "", f.titles.notFound, false
	}
	if typed == "~" || strings.HasPrefix(typed, "~/") {
		if src.Home == "" {
			return "", f.titles.noHome, false
		}
		typed = strings.TrimSuffix(src.Home, "/") + "/" + strings.TrimPrefix(strings.TrimPrefix(typed, "~"), "/")
	}
	if strings.HasPrefix(typed, src.Root) || strings.HasPrefix(typed, "/") {
		p, ok = src.fsPath(typed)
	} else {
		p = path.Clean(path.Join(dir, typed))
		ok = fs.ValidPath(p)
	}
	if !ok {
		return "", f.titles.notFound, false
	}
	return p, tui.Message{}, true
}

// check finds out what p names and runs then with it: at once for a source read on the loop, and
// off the loop for an async one, then told it ran later. ctx is the listing's (the FileOpenView's),
// whose HandleEvent the answer comes back to. still says whether the answer is still wanted when
// it comes (the field not edited since, the same folder listed); an edit, or the field following
// the listing elsewhere, drops it anyway.
func (f *pathField) check(ctx *tui.Context, src FileSource, p string, still func() bool, then func(k pathKind, later bool)) {
	if k, ok := f.stat.known(p); ok {
		then(k, false)
		return
	}
	if !src.async() || ctx == nil {
		then(src.kind(context.Background(), p), false)
		return
	}
	f.stat.ask(ctx, src, p, func(k pathKind) {
		if still() {
			then(k, true)
		}
	})
}

// titleFor is the field's title for a path that names k where the view wanted something else.
func (f *pathField) titleFor(k pathKind) tui.Message {
	switch k {
	case pathMissing:
		return f.titles.notFound
	case pathUnreadable:
		return f.titles.cannotRead
	}
	return f.titles.notFolder
}
