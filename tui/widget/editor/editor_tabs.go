package editor

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// THE EDITOR'S TABS — the host's tabs (an editor's buffers, say) on the cell Editor's frame's title
// row, at its left, where a panel carries its tabs and gui's Editor has the same strip and model
// on its window's title bar (gui/widget.Editor.Tabs). The row's right keeps Raw | Rendered, and the
// title, after the tabs, the room left. The host feeds the strip and hears its choices; the Editor
// only hands it to the frame (FrameTabs; decl's Frame holds it for its Editor). Off by default: an
// editor whose tabs are off, or whose strip has no tabs, gives the row no room.

// Tabs is the strip the editor's frame shows on its title row while SetTabsInFrame is on: the
// host's tabs, as gui's Editor.Tabs is on its title bar. The same strip every call.
func (e *Editor) Tabs() *core.TabStrip {
	if e.tabs == nil {
		e.tabs = core.NewTabStrip()
		e.tabs.SetVisible(e.tabsFrame) // the frame's title row shows it only while on
	}
	return e.tabs
}

// SetTabsInFrame shows the editor's tabs (Tabs) on its frame's title row, at its left: the frame
// holds FrameTabs there (widget.Box.SetTitleLeadSlot; decl's Frame does so for its Editor). Off, the
// strip is hidden and takes no room. Off by default.
func (e *Editor) SetTabsInFrame(on bool) {
	if e.tabsFrame == on {
		return
	}
	e.tabsFrame = on
	if e.tabs != nil {
		e.tabs.SetVisible(on) // hidden, it takes no room on the row (tui/visibility.go)
	}
}

// TabsInFrame reports whether the editor's tabs show on its frame's title row (SetTabsInFrame).
func (e *Editor) TabsInFrame() bool { return e.tabsFrame }

// FrameTabs is the strip as a frame's title row holds it (widget.Box.SetTitleLeadSlot): Tabs
// itself, as wide as its tabs need while SetTabsInFrame is on, and hidden (no width) while it is
// off: the strip's own visibility (Base.SetVisible), not a wrapper round it, since a type embedding
// TabStrip would make it an embeddable base, whose own calls an embedder's methods could bypass.
func (e *Editor) FrameTabs() tui.Component { return e.Tabs() }
