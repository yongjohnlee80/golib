package widget

import "github.com/yongjohnlee80/golib/tui"

// modMask are the modifiers that veto text insertion.
const nonTextMods = tui.ModCtrl | tui.ModAlt | tui.ModSuper | tui.ModHyper | tui.ModMeta
