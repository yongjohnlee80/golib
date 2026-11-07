package widget

import "github.com/yongjohnlee80/golib/tui"

// THE EDITOR'S TABS — the host's tabs (an editor's buffers, say) on the cell Editor's frame's
// bottom border, the same strip and model as gui's Editor has on its window's title bar
// (gui/widget.Editor.Tabs), kept off the frame's title row, which a terminal's host fills with the
// title, its badges and Raw | Rendered. The host feeds the strip and hears its choices; the Editor
// only hands it to the frame (FrameTabs; decl's Frame holds it for its Editor). Off by default: an
// editor whose tabs are off, or whose strip has no tabs, gives the border no room.

// Tabs is the strip the editor's frame shows on its bottom border while SetTabsInFrame is on: the
// host's tabs, as gui's Editor.Tabs is on its title bar. The same strip every call.
func (e *Editor) Tabs() *TabStrip {
	if e.tabs == nil {
		e.tabs = NewTabStrip()
		e.tabs.SetVisible(e.tabsFrame) // the frame's border shows it only while on
	}
	return e.tabs
}

// SetTabsInFrame shows the editor's tabs (Tabs) on its frame's bottom border: the frame holds
// FrameTabs there (widget.Box.SetBottomSlot; decl's Frame does so for its Editor). Off, the strip is
// hidden and takes no room. Off by default.
func (e *Editor) SetTabsInFrame(on bool) {
	if e.tabsFrame == on {
		return
	}
	e.tabsFrame = on
	if e.tabs != nil {
		e.tabs.SetVisible(on) // hidden, it takes no room on the row (tui/visibility.go)
	}
}

// TabsInFrame reports whether the editor's tabs show on its frame's bottom border (SetTabsInFrame).
func (e *Editor) TabsInFrame() bool { return e.tabsFrame }

// FrameTabs is the strip as a frame's bottom border holds it (widget.Box.SetBottomSlot): Tabs
// itself, as wide as its tabs need while SetTabsInFrame is on, and hidden (no width) while it is
// off: the strip's own visibility (Base.SetVisible), not a wrapper round it, since a type embedding
// TabStrip would make it an embeddable base, whose own calls an embedder's methods could bypass.
func (e *Editor) FrameTabs() tui.Component { return e.Tabs() }
