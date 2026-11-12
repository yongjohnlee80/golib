package editor

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// THE EDITOR'S TABS — the host's tabs (an editor's buffers, say) on the first row inside the cell
// Editor's frame, under its title row, as a panel of terminals carries its tabs on its first row
// (terminal.TerminalTabs); gui's Editor has the same strip and model on its window's title bar
// (gui/widget.Editor.Tabs). The title row keeps the title and Raw | Rendered whole. The host feeds
// the strip and hears its choices; the Editor only hands it to the frame (FrameTabs; decl's Frame
// holds it for its Editor). Off by default: an editor whose tabs are off takes no row for them.

// Tabs is the strip the editor's frame shows on its first row while SetTabsInFrame is on: the
// host's tabs, as gui's Editor.Tabs is on its title bar. The same strip every call.
func (e *Editor) Tabs() *core.TabStrip {
	if e.tabs == nil {
		e.tabs = core.NewTabStrip()
		e.tabs.SetVisible(e.tabsFrame) // the frame shows its row only while on
	}
	return e.tabs
}

// SetTabsInFrame shows the editor's tabs (Tabs) on the first row inside its frame, the editor a row
// shorter under it: the frame holds FrameTabs there (widget.Box.SetHeaderSlot; decl's Frame does so
// for its Editor). Off, the strip is hidden and takes no row. Off by default.
func (e *Editor) SetTabsInFrame(on bool) {
	if e.tabsFrame == on {
		return
	}
	e.tabsFrame = on
	if e.tabs != nil {
		e.tabs.SetVisible(on) // hidden, it takes no row (tui/visibility.go)
	}
}

// TabsInFrame reports whether the editor's tabs show in its frame (SetTabsInFrame).
func (e *Editor) TabsInFrame() bool { return e.tabsFrame }

// FrameTabs is the strip as a frame holds it (widget.Box.SetHeaderSlot, the first row inside it):
// Tabs itself, shown while SetTabsInFrame is on, and hidden (no row) while it is off: the strip's
// own visibility (Base.SetVisible), not a wrapper round it, since a type embedding
// TabStrip would make it an embeddable base, whose own calls an embedder's methods could bypass.
func (e *Editor) FrameTabs() tui.Component { return e.Tabs() }
