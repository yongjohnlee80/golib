package decl

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// CheckKeymap checks a program's keys without running it, as Check checks its layout: it builds the
// key map (golib's commands, the host's KeyCommands, every Keymap layer), mounts the layout once and
// builds no App. Its error is every problem that keeps a key from working:
//   - what keymap.Build refuses: a command declared twice, a scheme id nobody declared, two commands
//     of one scope on one key, a malformed scheme (file and line);
//   - a keyId Shortcut naming no declared command, in a holder with no keyScope, or in a holder whose
//     scope is not its command's;
//   - a fixed Shortcut sequence that overlaps a command key of its holder's scope (FixedKey);
//   - a Global command key that overlaps a top-level menu's Alt+letter, which the Window would fire
//     before the menu could open.
//
// warnings are what keymap.Build warns of: an inner key shared with Global or App, which the inner
// stop wins while focus is there. Johno's decision makes that legal, so it is never in err.
func CheckKeymap(opts ...ProgramOption) (warnings []keymap.Problem, err error) {
	c, err := configure(opts)
	if err != nil {
		return nil, err
	}
	spec, err := qml.QML{File: c.layoutFile}.Parse(c.layout)
	if err != nil {
		return c.keys.Warnings(), err
	}
	p, err := mount(c, spec)
	if err != nil {
		return c.keys.Warnings(), err
	}
	defer func() { _ = p.tree.Destroy() }()
	var problems []string
	if err := p.adapter.checkShortcuts(c.keys); err != nil {
		problems = append(problems, strings.TrimPrefix(err.Error(), "keymap: "))
	}
	problems = append(problems, fixedKeyProblems(p.adapter, c.keys)...)
	problems = append(problems, mnemonicProblems(p.adapter, c.keys)...)
	if len(problems) == 0 {
		return c.keys.Warnings(), nil
	}
	slices.Sort(problems)
	return c.keys.Warnings(), errors.New("keymap: " + strings.Join(problems, "; "))
}

// fixedKeyProblems is each fixed Shortcut sequence that overlaps a command key of its holder's scope:
// one of the two would never fire.
func fixedKeyProblems(a *Adapter, m *keymap.Map) []string {
	var out []string
	for _, b := range a.nodes {
		scope, shortcuts, ok := holderOf(b.comp)
		if !ok || scope == "" {
			continue
		}
		for _, sc := range shortcuts {
			if sc.key.command != "" {
				continue
			}
			for _, bnd := range m.Bindings(scope) {
				for _, k := range bnd.Keys {
					if key.Overlaps(sc.key.seq, k) {
						out = append(out, fmt.Sprintf("the fixed sequence %s overlaps %s of %q in %s (at %s)", sc.key.seq, k, bnd.Command.ID, scope, sc.pos))
					}
				}
			}
		}
	}
	return out
}

// mnemonicProblems is each Global command key that overlaps Alt and a top-level menu's mnemonic.
func mnemonicProblems(a *Adapter, m *keymap.Map) []string {
	var out []string
	for _, b := range a.nodes {
		bar, ok := b.comp.(*menuBarNode)
		if !ok {
			continue
		}
		for _, row := range bar.rows {
			r := row.model.Hotkey
			if r == 0 {
				continue
			}
			alt := key.Seq{Code: unicode.ToLower(r), Mods: key.Alt}
			for _, bnd := range m.Bindings(keymap.Global) {
				for _, k := range bnd.Keys {
					if key.Overlaps(alt, k) {
						out = append(out, fmt.Sprintf("%s of %q takes Alt+%c, the menu %q's mnemonic: the Window fires it first and the menu never opens",
							k, bnd.Command.ID, unicode.ToUpper(r), row.model.Label))
					}
				}
			}
		}
	}
	return out
}
