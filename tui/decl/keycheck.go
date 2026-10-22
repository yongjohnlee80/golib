package decl

import (
	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A KEYS-ONLY MOUNT: what a reload checks its keyId Shortcuts on (Program.checkReloadKeys), apart
// from the live tree. The layout is mounted, so components expand and bound values (a holder's
// keyScope from a source) resolve, but it is never shown: no type's Build, setters, palette or
// Destroyed runs for it, a consumer's least of all. A consumer's Build may reach shared state (a
// view it records for its host, as AutoDoc's GlobeMap does), and a check that ran it, then destroyed
// what it built, left the live tree's record cleared. checkShortcuts reads two things, each holder's
// keyScope and its Shortcuts, so that is all this mount builds:
//   - a Shortcut, built as ever: its build reads its sequence and touches nothing outside itself;
//   - a shortcut holder (shortcutHolders), a keyHolder: its keyScope as declared, Global for a
//     Window, and its Shortcut children;
//   - any other type, an inertNode.

// keysOnly makes the adapter build a keys-only mount.
func keysOnly() Option { return func(a *Adapter) { a.keysOnly = true } }

// keyHolder is a shortcut holder in a keys-only mount.
type keyHolder struct {
	inertNode
	scope     keymap.Scope
	shortcuts []*shortcutNode
}

func (h *keyHolder) heldShortcuts() (keymap.Scope, []*shortcutNode) { return h.scope, h.shortcuts }

// inertNode is any other type in a keys-only mount. It is never laid out or drawn: the mount has no
// App.
type inertNode struct{ widget.Base }

func (*inertNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*inertNode) Render(tui.Surface)                {}

// createKeysOnly is Create in a keys-only mount, for every type but a Shortcut. Every property is
// reported consumed, so the engine applies none.
func (a *Adapter) createKeysOnly(c decl.Construction) ([]string, error) {
	consumed := make([]string, 0, len(c.Props))
	for _, p := range c.Props {
		consumed = append(consumed, p.Name)
	}
	var b built
	if shortcutHolders[c.Type] {
		h := &keyHolder{scope: keymap.Global}
		if c.Type != windowType.Name {
			h.scope = ""
			for _, p := range c.Props {
				if p.Name != keyScopeProp {
					continue
				}
				s, err := scopeOf(p.Value)
				if err != nil {
					return nil, err
				}
				h.scope = s
			}
		}
		for _, id := range c.Children {
			if sc, ok := a.nodes[id].comp.(*shortcutNode); ok {
				h.shortcuts = append(h.shortcuts, sc)
			}
		}
		b.comp = h
	} else {
		b.comp = &inertNode{}
	}
	b.typ, b.pos = c.Type, c.Pos
	a.nodes[c.Node] = b
	a.kids[c.Node] = append([]decl.NodeID(nil), c.Children...)
	return consumed, nil
}

var _ shortcutHolder = (*keyHolder)(nil)
