package decl

import (
	"fmt"
	"slices"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/key"
)

// SHORTCUTS.
//
//	Window {
//	    Shortcut { sequence: "Ctrl+Q"; onActivated: App.exit() }
//	}
//
// QML's own type, spelled as Qt spells it: a `sequence` and an `onActivated`.
// A Shortcut is DATA, like a menu row — the Window that contains it owns the
// keys, because a shortcut has to fire whichever widget holds focus, and only
// an ancestor of every widget sees every key.

// shortcutMods are the modifiers a Window's own keys (F10, Alt+mnemonic) read.
const shortcutMods = tui.ModShift | tui.ModAlt | tui.ModCtrl

// seqMatches reports whether a key press is s, by the one matching model every binding uses: the
// event's chord (key.Of), then each of its candidates in order (key.Candidates). A release never
// matches, and an event holding Hyper or Meta matches nothing.
func seqMatches(s key.Seq, ev tui.KeyEvent) bool {
	if ev.Kind != tui.KeyPress {
		return false
	}
	chord, ok := key.Of(ev.Code, ev.Mods, ev.Text)
	return ok && slices.Contains(key.Candidates(chord), s)
}

// shortcutNode is a declared Shortcut, before a Window adopts it.
type shortcutNode struct {
	key     shortcutKey // a fixed key, or a key command's (keyId)
	pos     string      // where it was written, for a refusal
	trigger func()
}

func (*shortcutNode) Init(*tui.Context)               {}
func (*shortcutNode) Layout(tui.Constraints) tui.Size { return tui.Size{} }
func (*shortcutNode) DeclarationOnly()                {}
func (*shortcutNode) Render(tui.Surface)              {}
func (*shortcutNode) HandleEvent(tui.Event) bool      { return false }

func buildShortcut(b Build) (tui.Component, []string, error) {
	var k shortcutKey
	consumed, err := b.ReadProps(map[string]field{"sequence": into(&k, shortcutKeyOf)})
	if err != nil {
		return nil, nil, err
	}
	if k.command == "" && k.seq == (key.Seq{}) {
		return nil, nil, fmt.Errorf("Shortcut needs a sequence (at %s)", b.Pos)
	}
	return &shortcutNode{key: k, pos: fmt.Sprint(b.Pos), trigger: b.Emitter("activated")}, consumed, nil
}

// splitShortcuts parts a Shortcut holder's children into its Shortcuts and the rest, each in its
// order: the one partition every holder makes (FocusScope, Drawer, Dialog, FolderDialog).
func splitShortcuts(children []tui.Component) (shortcuts []*shortcutNode, rest []tui.Component) {
	for _, c := range children {
		if sc, ok := c.(*shortcutNode); ok {
			shortcuts = append(shortcuts, sc)
		} else {
			rest = append(rest, c)
		}
	}
	return shortcuts, rest
}

// oneContent is a holder's one content item among the rest of its children, refusing none or more,
// in one wording: "a Drawer holds exactly one content item besides its Shortcuts, got 2 (at …)".
func oneContent(rest []tui.Component, owner string, pos fmt.Stringer) (tui.Component, error) {
	if len(rest) != 1 {
		return nil, fmt.Errorf("a %s holds exactly one content item besides its Shortcuts, got %d (at %s)", owner, len(rest), pos)
	}
	return rest[0], nil
}

// shortcutType is the Shortcut type.
var shortcutType = Type{Name: "Shortcut", Build: buildShortcut, Ctor: []string{"sequence"}}
