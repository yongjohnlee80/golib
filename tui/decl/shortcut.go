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
// matches, and an event holding Hyper or Meta matches nothing (ADR 1792255248 §4.2).
func seqMatches(s key.Seq, ev tui.KeyEvent) bool {
	if ev.Kind != tui.KeyPress {
		return false
	}
	chord, ok := key.Of(ev.Code, ev.Mods, ev.Text)
	return ok && slices.Contains(key.Candidates(chord), s)
}

// shortcutNode is a declared Shortcut, before a Window adopts it.
type shortcutNode struct {
	seq     key.Seq
	trigger func()
}

func (*shortcutNode) Init(*tui.Context)               {}
func (*shortcutNode) Layout(tui.Constraints) tui.Size { return tui.Size{} }
func (*shortcutNode) declarationOnly()                {}
func (*shortcutNode) Render(tui.Surface)              {}
func (*shortcutNode) HandleEvent(tui.Event) bool      { return false }

func buildShortcut(b Build) (tui.Component, []string, error) {
	var text string
	consumed, err := readProps(b.Props, map[string]field{"sequence": into(&text, stringOf)})
	if err != nil {
		return nil, nil, err
	}
	if text == "" {
		return nil, nil, fmt.Errorf("Shortcut needs a sequence (at %s)", b.Pos)
	}
	seq, err := key.Parse(text)
	if err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}
	return &shortcutNode{seq: seq, trigger: b.Emitter("activated")}, consumed, nil
}
