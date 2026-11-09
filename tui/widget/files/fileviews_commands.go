package files

import (
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// A file chooser's key commands (declared through internal/kit's Cmd and its helpers).

// FileViewScope is a file chooser's own keys, heard as they bubble up from its parts. Each acts only
// where it belongs: ~ and / in the listing, Enter in the filter field, Esc in the path field or with a
// filter to clear, and Ctrl and h, j, k or l among the parts.
const FileViewScope keymap.Scope = "tui.fileview"

var (
	FileViewHome       = kit.Cmd("tui.fileview.home", FileViewScope, "~")                                      // the path field, ~/ written
	FileViewFilter     = kit.Cmd("tui.fileview.filter", FileViewScope, "/")                                    // the filter field
	FileViewFilterDone = kit.Cmd("tui.fileview.filterDone", FileViewScope, kit.Mods("Enter", kit.AnyCombo)...) // back to the listing, the filter kept
	FileViewCancel     = kit.Cmd("tui.fileview.cancel", FileViewScope, kit.Mods("Esc", kit.AnyCombo)...)       // the path field's folder put back, else the filter cleared
	FileViewPartDown   = kit.Cmd("tui.fileview.partDown", FileViewScope, "Ctrl+J")
	FileViewPartUp     = kit.Cmd("tui.fileview.partUp", FileViewScope, "Ctrl+K")
	FileViewPreview    = kit.Cmd("tui.fileview.preview", FileViewScope, "Ctrl+L")
	FileViewBack       = kit.Cmd("tui.fileview.back", FileViewScope, "Ctrl+H") // from the preview

	fileViewCommands = []keymap.Command{FileViewHome, FileViewFilter, FileViewFilterDone, FileViewCancel,
		FileViewPartDown, FileViewPartUp, FileViewPreview, FileViewBack}
)
