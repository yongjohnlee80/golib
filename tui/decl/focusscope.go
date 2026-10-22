package decl

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
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
// pointer, so the Tab order and what a click lands on are those of the content alone. Its parent
// lays out the FocusScope, so Layout and Dock properties are written on it, as in Qt, not on its
// content: wrapping an item moves them to the scope.
//
// A RELOAD edits it in place, as a Window: its children are sorted into content and keys again on
// each change, so a Shortcut added, removed or replaced is live or gone at once, and content that is
// still there stays mounted.

// focusScopeNode is a FocusScope: its content, and the keys live within it.
type focusScopeNode struct {
	tui.MultiChild
	ctx       *tui.Context
	shortcuts []*shortcutNode
	// keyScope is the key-map scope of its keyId Shortcuts (keymap.Scope): "editor.todos".
	keyScope keymap.Scope
}

func buildFocusScope(b Build) (tui.Component, []string, error) {
	if _, rest := splitShortcuts(b.Children); len(rest) != 1 {
		_, err := oneContent(rest, "FocusScope", b.Pos)
		return nil, nil, err
	}
	s := &focusScopeNode{}
	s.Label("FocusScope")
	if err := s.arrange(b.Children, nil, nil); err != nil {
		return nil, nil, err
	}
	return s, nil, nil
}

// arrange sorts the children into the keys and the content, mounting a content item that came and
// unmounting one that went. A reload removes before it inserts, so a replaced content leaves the
// scope empty for a moment; two content items are refused.
func (s *focusScopeNode) arrange(children []tui.Component, _ []map[string]qml.SpecValue, _ tui.Component) error {
	shortcuts, rest := splitShortcuts(children)
	if len(rest) > 1 {
		return fmt.Errorf("a FocusScope holds exactly one content item besides its Shortcuts, got %d", len(rest))
	}
	var content tui.Component
	if len(rest) == 1 {
		content = rest[0]
	}
	for _, c := range append([]tui.Component(nil), s.Items()...) {
		if c != content {
			s.Remove(c)
		}
	}
	if content != nil && len(s.Items()) == 0 {
		s.Add(content)
	}
	s.shortcuts = shortcuts
	return nil
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
func (s *focusScopeNode) HandleEvent(ev tui.Event) bool {
	return fireShortcut(s.ctx, s.keyScope, s.shortcuts, ev)
}

// KeyScope is the scope its keyId Shortcuts belong to (tui.KeyScoped).
func (s *focusScopeNode) KeyScope() keymap.Scope { return s.keyScope }

// KeyBindings is its Shortcuts as m binds them (tui.KeyBinder).
func (s *focusScopeNode) KeyBindings(m *keymap.Map) []keymap.Binding {
	return shortcutBindings(m, s.keyScope, s.shortcuts)
}

func (s *focusScopeNode) heldShortcuts() (keymap.Scope, []*shortcutNode) {
	return s.keyScope, s.shortcuts
}

func (s *focusScopeNode) setKeyScope(v string) { s.keyScope = keymap.Scope(v) }

// shortcutHolders are the types a Shortcut may be declared in: each makes the keys live while
// focus is within it, and builds a shortcutHolder.
var shortcutHolders = map[string]bool{"Window": true, "Dialog": true, "FolderDialog": true, "Drawer": true, "FocusScope": true}

// shortcutHolderNames is shortcutHolders as a refusal lists them: "Dialog, Drawer, … or Window".
func shortcutHolderNames() string {
	names := slices.Sorted(maps.Keys(shortcutHolders))
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// focusScopeType is the FocusScope's entry in the standard vocabulary.
var focusScopeType = Type{
	Name:    "FocusScope",
	Build:   buildFocusScope,
	Setters: map[string]Setter{"keyScope": setter("a FocusScope", stringOf, (*focusScopeNode).setKeyScope)},
	Getters: map[string]Getter{"keyScope": StringGetter(func(s *focusScopeNode) string { return string(s.keyScope) })},
}
