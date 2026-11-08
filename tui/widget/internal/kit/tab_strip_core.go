package kit

import "github.com/yongjohnlee80/golib/tui"

// StripTab is one tab as the strip draws it: its id, and its label as drawn (CellLabel's padding
// and any status mark included).
type StripTab struct {
	ID    string
	Label string
}

const (
	CellTab     StripKind = iota // a tab
	CellPrev                     // ←, the tabs before the window
	CellNext                     // →, the tabs after it
	CellAdd                      // +
	CellMenu                     // ≡
	CellControl                  // a window control at the row's right end (SetWindowButtons)
)

// TabWindowCommitKey is a tab row's window commit (tui.Context.AfterLayout), the same for every
// owner of a strip: TabStrip, Tabs and TerminalTabs.
const TabWindowCommitKey tui.CommitKey = "tab-window"

// StripKind is what a stretch of the row is.
type StripKind uint8
