package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

// FOCUS SCOPES — QtQuick's FocusScope, as golib uses it: the element whose Shortcuts are live while
// focus is inside it.
//
//	FocusScope {
//	    Shortcut { sequence: "n"; onActivated: App.findNext() }
//	    Editor { … }
//	}
//
// A Shortcut is live while focus is inside the element that declares it: a Window's everywhere, a
// Dialog's while it is open, a Drawer's or a FocusScope's while focus is within it. A key reaches a
// scope's Shortcuts once the focused widget has left it, before any scope around this one and the
// Window, so the innermost claim wins and a field still types its letters.
//
// TRANSPARENT: it holds one content item and is otherwise not there. The content is laid out as the
// FocusScope's parent lays the FocusScope; nothing is painted; the scope takes neither focus nor the
// pointer, so the Tab order and what a click lands on are those of the content alone. A FocusScope
// with no Layout or Dock properties of its own takes its content's, so wrapping an item leaves where
// it sits unchanged.

// focusScopeNode is a FocusScope: its content, and the keys live within it.
type focusScopeNode struct {
	tui.MultiChild
	ctx       *tui.Context
	shortcuts []*shortcutNode
}

func buildFocusScope(b Build) (tui.Component, []string, error) {
	var content tui.Component
	var shortcuts []*shortcutNode
	n := 0
	for _, c := range b.Children {
		if sc, ok := c.(*shortcutNode); ok {
			shortcuts = append(shortcuts, sc)
			continue
		}
		content = c
		n++
	}
	if n != 1 {
		return nil, nil, fmt.Errorf("a FocusScope holds exactly one content item besides its Shortcuts, got %d (at %s)", n, b.Pos)
	}
	s := &focusScopeNode{shortcuts: shortcuts}
	s.Label("FocusScope")
	s.Add(content)
	return s, nil, nil
}

func (s *focusScopeNode) Init(ctx *tui.Context) {
	s.ctx = ctx
	s.MultiChild.Init(ctx)
}

// Layout gives the content what the FocusScope is given, and is the content's size.
func (s *focusScopeNode) Layout(c tui.Constraints) tui.Size {
	var sz tui.Size
	for _, child := range s.Items() {
		sz = s.ctx.LayoutChild(child, c)
		s.ctx.PlaceChild(child, tui.Rect{W: sz.W, H: sz.H})
	}
	return c.Constrain(sz)
}

func (s *focusScopeNode) Render(tui.Surface) {}

// HandleEvent fires the scope's Shortcut for a key its content left.
func (s *focusScopeNode) HandleEvent(ev tui.Event) bool { return fireShortcut(s.shortcuts, ev) }

// fireShortcut fires the first of shortcuts that ev is, and reports whether one was.
func fireShortcut(shortcuts []*shortcutNode, ev tui.Event) bool {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind != tui.KeyPress {
		return false
	}
	for _, s := range shortcuts {
		if s.seq.matches(k) {
			s.trigger()
			return true
		}
	}
	return false
}

// shortcutHolders are the types a Shortcut may be declared in: each makes the keys live while
// focus is within it.
var shortcutHolders = map[string]bool{"Window": true, "Dialog": true, "FolderDialog": true, "Drawer": true, "FocusScope": true}

// focusScopeType is the FocusScope's entry in the standard vocabulary.
var focusScopeType = Type{Name: "FocusScope", Build: buildFocusScope}
