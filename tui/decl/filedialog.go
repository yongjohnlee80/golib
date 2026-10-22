package decl

import (
	"fmt"
	"slices"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// FILE DIALOGS — Qt's FileDialog, over golib's file views.
//
// It lists through the adapter's [widget.FileSource] — the local disk unless
// the host gave another with [WithFileSource] — so the same document browses a
// remote store by the host's choosing, not by anything written in QML.
//
//	FileDialog {
//	    title: "Open"
//	    fileMode: Tui.OpenFile            // or Tui.SaveFile
//	    currentFolder: App.folder
//	    selectedFile: App.path            // where "Save As" starts
//	    preview: false                    // on by default when opening
//	    onAccepted: App.openFile(selectedFile)
//	}
//
// A Dialog like any other — the same card, the same buttons-close-it lifecycle
// through [newDialog] — whose body is a widget.FileChooser, and whose `accepted`
// carries the file chosen, as Qt's selectedFile. Opening a folder is not a
// choice: Enter or the Open button on a folder goes into it and the dialog
// stays, which the browser decides and the button asks it.
//
// The preview is HIGHLIGHTED: each file by the definition its name calls for
// (the registered definitions' extensions — KSyntaxHighlighting's
// definitionForFileName), in the `syntax.*` roles the dialog inherits. A
// theme that sets none leaves it plain.
//
// The footer follows the keyboard, unless a helpText is given: what Enter does
// in the listing is not what it does in the preview, and a footer listing
// every key for every pane at once says neither.

// fileMode is one of Qt's FileDialog.fileMode values: which view the dialog
// holds, and what its choosing button says. A new mode is an entry here.
type fileMode struct {
	choose standardButton
	view   func(...widget.FileViewOption) hintedChooser
}

// hintedChooser is a file view that names its footer hint as a catalog message, as every
// view a FileDialog holds does, so the footer follows the App's language while it is open.
type hintedChooser interface {
	widget.FileChooser
	HintMessage() tui.Message
}

// nameClearer is a file view with a name field (FileSaveView), which each opening empties.
type nameClearer interface{ SetName(string) }

// sourceSetter is a file view whose files can be replaced (the source property): every view a
// dialog holds.
type sourceSetter interface{ SetSource(widget.FileSource) }

var fileModeTable = map[string]fileMode{
	"OpenFile": {standardButton{name: "Open", msg: tui.Msg("tui.filedialog.open"), accept: true},
		func(o ...widget.FileViewOption) hintedChooser { return widget.NewFileOpenView(o...) }},
	"SaveFile": {standardButton{name: "Save", msg: tui.Msg("tui.filedialog.save"), accept: true},
		func(o ...widget.FileViewOption) hintedChooser { return widget.NewFileSaveView(o...) }},
}

var fileModes = enum[fileMode]{values: fileModeTable}

func buildFileDialog(b Build) (tui.Component, []string, error) {
	c := chooserSpec{spec: dialogSpec{dim: true, align: widget.ButtonsRight}}
	mode := fileModeTable["OpenFile"]
	consumed, err := b.ReadProps(map[string]field{
		"title":    into(&c.spec.title, textOf),
		"helpText": into(&c.spec.help, textOf),
		"dim":      into(&c.spec.dim, boolOf),
		"fileMode": into(&mode, fileModes.read),
		"preview":  into(&c.preview, boolOf),
	})
	if err != nil {
		return nil, nil, err
	}
	// Open (or Save) is the default, as QFileDialog's is: Enter chooses.
	c.choose, c.view, c.previewSet = mode.choose, mode.view, slices.Contains(consumed, "preview")
	return newChooserDialog(b, c), consumed, nil
}

// chooserSpec is what a picker dialog's builder reads; newChooserDialog builds the rest, the same
// for every picker.
type chooserSpec struct {
	spec                dialogSpec
	preview, previewSet bool            // `preview`, and whether it was written
	fields              []tui.Component // above the path, FolderDialog's children
	choose              standardButton  // the choosing button, the default
	view                func(...widget.FileViewOption) hintedChooser
}

// newChooserDialog is a picker dialog: c's view with the program's files, choosing through the
// dialog's accept; its footer the view's hint unless helpText fixes it; Close (q outside a field)
// and c's choosing button, the default; and each opening listing the folder afresh. One builder,
// so a fix to a picker's lifecycle reaches FileDialog and FolderDialog alike.
func newChooserDialog(b Build, c chooserSpec) *dialogNode {
	s := c.spec
	fixedHelp := !s.help.Empty()
	var d *dialogNode
	var chooser hintedChooser
	// A path an async source checks off the loop is chosen when the check answers, through
	// onChoose, past the gate that turned it away.
	opts := []widget.FileViewOption{widget.WithFileViewSource(b.Files), widget.WithOnChoose(func() { d.accept() })}
	if len(c.fields) > 0 {
		opts = append(opts, widget.WithFileViewFields(c.fields...))
	}
	// `preview` is a vocabulary extension — Qt's FileDialog has no preview —
	// and when it is not written the view's own default holds.
	if c.previewSet {
		opts = append(opts, widget.WithFileViewPreview(c.preview))
	}
	if !fixedHelp {
		opts = append(opts, widget.WithOnHint(func(string) { showHint(d.modal, chooser) }))
	}
	chooser = c.view(opts...)
	s.body = chooser
	s.buttons, s.defaultAt = []standardButton{closeButton, c.choose}, 1
	s.closeOnQ = true
	s.hooks = dialogHooks{
		gate: chooser.Confirm,
		opened: func() {
			// The folder is listed AFRESH each time: files come and go
			// between one opening and the next. A selectedFile the document
			// bound is placed again, so the dialog starts from it every time
			// rather than from whatever was typed and cancelled last. With
			// none bound, a save view's name starts empty for the same reason.
			if d.selected != "" {
				chooser.Select(d.selected)
			} else {
				chooser.SetDir(chooser.Dir())
				if n, ok := chooser.(nameClearer); ok {
					n.SetName("")
				}
			}
			chooser.FocusInitial()
			if !fixedHelp {
				showHint(d.modal, chooser)
			}
		},
		acceptArgs: func() []qml.SpecValue { return []qml.SpecValue{strValue(chooser.Selected())} },
	}
	d = newDialog(b, s)
	d.chooser, d.highlighters = chooser, b.SourceLanguages
	return d
}

// fileSourceOf reads a dialog's source: a widget.FileSource the host binds
// (tuidecl.Value carries one), which replaces the program's Files for this
// dialog alone. A VFS one is listed and previewed off the loop.
func fileSourceOf(v qml.SpecValue) (widget.FileSource, error) {
	if src, ok := v.Obj.(widget.FileSource); v.Kind == qml.SpecValueObject && ok {
		return src, nil
	}
	return widget.FileSource{}, fmt.Errorf("source takes a widget.FileSource the host binds, not a %s", v.Kind)
}

// showHint puts the keys the focused part of view answers to in the dialog's footer, as the
// view's message, so the footer follows App.SetLanguage while the dialog is open.
func showHint(m *widget.Modal, view hintedChooser) { m.SetFooterMessage(view.HintMessage()) }

// closeButton is every picker's way out: Close, which q also presses outside a
// field.
var closeButton = standardButton{name: "Close", msg: tui.Msg("tui.filedialog.close")}

// FOLDER DIALOGS — Qt 6's FolderDialog, over widget.FileFolderView: a path
// field that follows the listing, the listing and its preview, and Select.
//
//	FolderDialog {
//	    title: "add a workspace"
//	    currentFolder: App.home
//	    Frame { title: "title"; TextField { id: wsName; text: "untitled" } }
//	    onAccepted: App.addWorkspace(wsName.text, selectedFolder)
//	}
//
// Its CHILDREN are fields at the top of the left column, above the path — a
// vocabulary extension: Qt's FolderDialog takes none, and a program asking
// for a folder often needs a word about it too. Shortcuts among them are the
// dialog's keys, as a Dialog's are.
func buildFolderDialog(b Build) (tui.Component, []string, error) {
	c := chooserSpec{spec: dialogSpec{dim: true, align: widget.ButtonsRight}}
	consumed, err := b.ReadProps(map[string]field{
		"title":    into(&c.spec.title, textOf),
		"helpText": into(&c.spec.help, textOf),
		"dim":      into(&c.spec.dim, boolOf),
		"keyScope": into(&c.spec.keyScope, scopeOf),
		"preview":  into(&c.preview, boolOf),
	})
	if err != nil {
		return nil, nil, err
	}
	shortcuts, rest := splitShortcuts(b.Children)
	c.spec.shortcuts = append(c.spec.shortcuts, shortcuts...)
	for _, ch := range rest {
		if _, ok := ch.(*buttonBoxNode); ok {
			return nil, nil, fmt.Errorf("a FolderDialog's buttons are its own, Select and Close (at %s)", b.Pos)
		}
		c.fields = append(c.fields, ch)
	}
	// Select is the default: Enter in a field that leaves it unclaimed (a
	// name typed) chooses the folder the path holds.
	c.choose = standardButton{name: "Select", msg: tui.Msg("tui.filedialog.select"), accept: true}
	c.view = func(o ...widget.FileViewOption) hintedChooser { return widget.NewFileFolderView(o...) }
	c.previewSet = slices.Contains(consumed, "preview")
	return newChooserDialog(b, c), consumed, nil
}

// setSelected is selectedFile's setter: Qt 6's writable selectedFile, the
// file the dialog starts from. "" leaves it starting from its folder.
func (d *dialogNode) setSelected(file string) {
	d.selected = file
	if d.chooser != nil && file != "" {
		d.chooser.Select(file)
	}
}

// setSource is source's setter: the files this dialog lists, in place of the
// program's (tuidecl.Files). A rebinding lists the new source afresh.
func (d *dialogNode) setSource(src widget.FileSource) {
	if c, ok := d.chooser.(sourceSetter); ok {
		c.SetSource(src)
		if d.folder != "" {
			d.chooser.SetDir(d.folder)
		}
	}
}

// setFolder is currentFolder's setter.
func (d *dialogNode) setFolder(dir string) {
	d.folder = dir
	if d.chooser != nil && dir != "" {
		d.chooser.SetDir(dir)
	}
}

// highlighterFor is the highlighter a previewed file's name calls for, nil for
// none.
func (d *dialogNode) highlighterFor(name string) highlight.Highlighter {
	if def, ok := d.highlighters.DefinitionForFileName(name); ok {
		return def.NewSource(d.highlighters).Highlighter
	}
	return nil
}
