package decl

import (
	"fmt"

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
	view   func(...widget.FileViewOption) widget.FileChooser
}

var fileModeTable = map[string]fileMode{
	"OpenFile": {standardButton{name: "Open", label: "&Open", accept: true},
		func(o ...widget.FileViewOption) widget.FileChooser { return widget.NewFileOpenView(o...) }},
	"SaveFile": {standardButton{name: "Save", label: "&Save", accept: true},
		func(o ...widget.FileViewOption) widget.FileChooser { return widget.NewFileSaveView(o...) }},
}

var fileModes = enum[fileMode]{values: fileModeTable}

func buildFileDialog(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("FileDialog takes no children; its content is the file view (at %s)", b.Pos)
	}
	s := dialogSpec{dim: true, align: widget.ButtonsRight}
	mode := fileModeTable["OpenFile"]
	var preview bool
	consumed, err := readProps(b.Props, map[string]field{
		"title":    into(&s.title, stringOf),
		"helpText": into(&s.help, stringOf),
		"dim":      into(&s.dim, boolOf),
		"fileMode": into(&mode, fileModes.read),
		"preview":  into(&preview, boolOf),
	})
	if err != nil {
		return nil, nil, err
	}
	fixedHelp := s.help != ""

	var d *dialogNode
	opts := []widget.FileViewOption{
		widget.WithFileViewSource(b.Files),
		widget.WithOnChoose(func() { d.accept() }),
	}
	// `preview` is a vocabulary extension — Qt's FileDialog has no preview —
	// and when it is not written the view's own default holds.
	for _, name := range consumed {
		if name == "preview" {
			opts = append(opts, widget.WithFileViewPreview(preview))
		}
	}
	if !fixedHelp {
		opts = append(opts, widget.WithOnHint(func(h string) { d.modal.SetFooter(h) }))
	}
	chooser := mode.view(opts...)
	s.body = chooser
	s.buttons = []standardButton{{name: "Cancel", label: "&Cancel"}, mode.choose}
	s.hooks = dialogHooks{
		gate: chooser.Confirm,
		opened: func() {
			// The folder is listed AFRESH each time: files come and go
			// between one opening and the next. A selectedFile the document
			// bound is placed again, so the dialog starts from it every time
			// rather than from whatever was typed and cancelled last.
			if d.selected != "" {
				chooser.Select(d.selected)
			} else {
				chooser.SetDir(chooser.Dir())
			}
			chooser.FocusInitial()
			if !fixedHelp {
				d.modal.SetFooter(chooser.Hint())
			}
		},
		acceptArgs: func() []qml.SpecValue { return []qml.SpecValue{strValue(chooser.Selected())} },
	}
	d = newDialog(b, s)
	d.chooser, d.highlighters = chooser, b.highlighters
	return d, consumed, nil
}

// setSelected is selectedFile's setter: Qt 6's writable selectedFile, the
// file the dialog starts from. "" leaves it starting from its folder.
func (d *dialogNode) setSelected(file string) {
	d.selected = file
	if d.chooser != nil && file != "" {
		d.chooser.Select(file)
	}
}

// setFolder is currentFolder's setter.
func (d *dialogNode) setFolder(dir string) {
	if d.chooser != nil && dir != "" {
		d.chooser.SetDir(dir)
	}
}

// highlighterFor is the highlighter a previewed file's name calls for, nil for
// none.
func (d *dialogNode) highlighterFor(name string) highlight.Highlighter {
	if d.highlighters == nil {
		return nil
	}
	if def, ok := d.highlighters.DefinitionForFileName(name); ok {
		return def.Highlighter
	}
	return nil
}
