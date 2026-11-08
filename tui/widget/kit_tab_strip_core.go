package widget

import "github.com/yongjohnlee80/golib/tui"

// stripTab is one tab as the strip draws it: its id, and its label as drawn (cellLabel's padding
// and any status mark included).
type stripTab struct {
	id    string
	label string
}

const (
	cellTab     stripKind = iota // a tab
	cellPrev                     // ←, the tabs before the window
	cellNext                     // →, the tabs after it
	cellAdd                      // +
	cellMenu                     // ≡
	cellControl                  // a window control at the row's right end (SetWindowButtons)
)

// tabWindowCommitKey is a tab row's window commit (tui.Context.AfterLayout), the same for every
// owner of a strip: TabStrip, Tabs and TerminalTabs.
const tabWindowCommitKey tui.CommitKey = "tab-window"

// stripKind is what a stretch of the row is.
type stripKind uint8
