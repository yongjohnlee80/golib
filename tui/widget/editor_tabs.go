package widget

import "github.com/yongjohnlee80/golib/tui"

// THE EDITOR'S TABS — the host's tabs (an editor's buffers, say) on the cell Editor's frame's title
// row, as gui's Editor has them on its window's title bar (gui/widget.Editor.Tabs): the strip at the
// row's left, the title after it, Raw | Rendered at its right. The host feeds the strip and hears
// its choices; the Editor only hands it to the frame (TitleTabs; decl's Frame holds it for its
// Editor). Off by default: an editor whose tabs are off, or whose strip has no tabs, gives the
// title row no room.

// Tabs is the strip the editor's frame shows on its title row while SetTabsInTitle is on: the
// host's tabs, as gui's Editor.Tabs is on its title bar. The same strip every call.
func (e *Editor) Tabs() *TabStrip {
	if e.tabs == nil {
		e.tabs = NewTabStrip()
	}
	return e.tabs
}

// SetTabsInTitle shows the editor's tabs (Tabs) on its frame's title row: the frame holds TitleTabs
// at its title row's left (widget.Box.SetTitleLeading; decl's Frame does so for its Editor). Off,
// the strip takes no room there. Off by default.
func (e *Editor) SetTabsInTitle(on bool) {
	if e.tabsTitle == on {
		return
	}
	e.tabsTitle = on
	if e.tabsSlot != nil {
		e.tabsSlot.RequestLayout() // shown or not changes its width
		e.tabsSlot.MarkDirty()
	}
}

// TabsInTitle reports whether the editor's tabs show on its frame's title row (SetTabsInTitle).
func (e *Editor) TabsInTitle() bool { return e.tabsTitle }

// TitleTabs is the strip as a frame's title row holds it (widget.Box.SetTitleLeading): Tabs, as
// wide as its tabs need while SetTabsInTitle is on, and no width while it is off. The same
// component every call.
func (e *Editor) TitleTabs() tui.Component {
	if e.tabsSlot == nil {
		e.tabsSlot = &TabStripSlot{TabStrip: e.Tabs(), shown: func() bool { return e.tabsTitle }}
	}
	return e.tabsSlot
}

// TabStripSlot is a TabStrip on a frame's title row for its owner: the strip's own drawing and
// presses, and no width while its owner hides it.
type TabStripSlot struct {
	*TabStrip
	shown func() bool
}

// Layout is the strip's, or none while its owner hides it.
func (s *TabStripSlot) Layout(c tui.Constraints) tui.Size {
	if !s.shown() {
		return c.Constrain(tui.Size{})
	}
	return s.TabStrip.Layout(c)
}
