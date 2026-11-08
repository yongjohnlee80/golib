package kit

import "github.com/yongjohnlee80/golib/tui"

// modMask are the modifiers that veto text insertion.
const NonTextMods = tui.ModCtrl | tui.ModAlt | tui.ModSuper | tui.ModHyper | tui.ModMeta
