package widget

import "github.com/yongjohnlee80/golib/tui/keymap"

// A file chooser's key commands (commands.go declares cmd and the shared helpers).

// FileViewScope is a file chooser's own keys, heard as they bubble up from its parts. Each acts only
// where it belongs: ~ and / in the listing, Enter in the filter field, Esc in the path field or with a
// filter to clear, and Ctrl and h, j, k or l among the parts.
const FileViewScope keymap.Scope = "tui.fileview"

var (
	FileViewHome       = cmd("tui.fileview.home", FileViewScope, "~")                              // the path field, ~/ written
	FileViewFilter     = cmd("tui.fileview.filter", FileViewScope, "/")                            // the filter field
	FileViewFilterDone = cmd("tui.fileview.filterDone", FileViewScope, mods("Enter", anyCombo)...) // back to the listing, the filter kept
	FileViewCancel     = cmd("tui.fileview.cancel", FileViewScope, mods("Esc", anyCombo)...)       // the path field's folder put back, else the filter cleared
	FileViewPartDown   = cmd("tui.fileview.partDown", FileViewScope, "Ctrl+J")
	FileViewPartUp     = cmd("tui.fileview.partUp", FileViewScope, "Ctrl+K")
	FileViewPreview    = cmd("tui.fileview.preview", FileViewScope, "Ctrl+L")
	FileViewBack       = cmd("tui.fileview.back", FileViewScope, "Ctrl+H") // from the preview

	fileViewCommands = []keymap.Command{FileViewHome, FileViewFilter, FileViewFilterDone, FileViewCancel,
		FileViewPartDown, FileViewPartUp, FileViewPreview, FileViewBack}
)
