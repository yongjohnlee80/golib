package decl

import (
	"fmt"

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

var fileModes = enum[fileMode]{prop: "fileMode", values: fileModeTable}

func buildFileDialog(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("FileDialog takes no children; its content is the file view (at %s)", b.Pos)
	}
	s := dialogSpec{dim: true, align: widget.ButtonsRight, p: palette{}}
	mode := fileModeTable["OpenFile"]
	var preview bool
	consumed, err := readProps(b.Props, withPalette(map[string]field{
		"title":    into(&s.title, stringOf),
		"helpText": into(&s.help, stringOf),
		"dim":      into(&s.dim, boolOf),
		"fileMode": into(&mode, fileModes.read),
		"preview":  into(&preview, boolOf),
	}, s.p, fileDialogRoles))
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
	if st, ok := s.p.browserStyles(); ok {
		opts = append(opts, widget.WithFileViewStyles(st))
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
			// between one opening and the next.
			chooser.SetDir(chooser.Dir())
			chooser.FocusInitial()
			if !fixedHelp {
				d.modal.SetFooter(chooser.Hint())
			}
		},
		acceptArgs: func() []qml.SpecValue { return []qml.SpecValue{strValue(chooser.Selected())} },
	}
	d = newDialog(b, s)
	d.chooser = chooser
	return d, consumed, nil
}

// setFolder is currentFolder's setter.
func (d *dialogNode) setFolder(dir string) {
	if d.chooser != nil && dir != "" {
		d.chooser.SetDir(dir)
	}
}
