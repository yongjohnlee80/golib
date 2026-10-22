package decl

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// KEY MAPS — overridable keys.
//
//	Window {
//	    Shortcut { sequence: keyId("editor.file.save"); onActivated: App.save() }
//	    FocusScope {
//	        keyScope: "editor.todos"
//	        Shortcut { sequence: keyId("editor.todos.add"); onActivated: App.addTask() }
//	        TodoList { }
//	    }
//	    MenuBar { Menu { MenuItem { text: qsTrId("editor.menu.save"); shortcut: keyId("editor.file.save") } } }
//	}
//
// keyId("id") names a key command the host declared (KeyCommands): its keys are the App's key map's,
// so a user's key-mapping scheme (Keymap) moves it. Like qsTrId it yields a reference, not keys, so it
// is a pure function and an override needs no binding re-evaluated. A Shortcut's sequence takes it
// beside a fixed string, as Qt's takes a StandardKey beside text; a MenuItem's shortcut shows the
// command's current key. A keyId Shortcut belongs to its command's scope: a Window's is
// keymap.Global, a FocusScope's, Dialog's, FileDialog's, FolderDialog's or Drawer's is its keyScope.

// keyRef is what keyId yields: the command id, resolved by whoever holds it.
type keyRef struct{ id string }

// keyId is the key command named by its argument, as a document calls it.
func keyId(args []qml.SpecValue) (qml.SpecValue, error) {
	if len(args) != 1 || args[0].Kind != qml.SpecValueString || args[0].Raw == "" {
		return qml.SpecValue{}, fmt.Errorf("keyId takes one string, a key command's id: keyId(\"editor.file.save\")")
	}
	return qml.SpecValue{Kind: qml.SpecValueObject, Obj: keyRef{id: args[0].Raw}}, nil
}

// shortcutKey is a Shortcut's sequence: a fixed key, or a command's (keyId).
type shortcutKey struct {
	seq     key.Seq
	command string
}

// shortcutKeyOf reads a sequence: a string in Qt's spelling, or keyId("id").
func shortcutKeyOf(v qml.SpecValue) (shortcutKey, error) {
	if r, ok := v.Obj.(keyRef); v.Kind == qml.SpecValueObject && ok {
		return shortcutKey{command: r.id}, nil
	}
	if v.Kind != qml.SpecValueString {
		return shortcutKey{}, fmt.Errorf("want a key sequence or keyId(\"id\"), got %s (at %s)", v.Kind, v.Pos)
	}
	if v.Raw == "" {
		return shortcutKey{}, nil
	}
	s, err := parseSequence(v.Raw)
	if err != nil {
		return shortcutKey{}, fmt.Errorf("%w (at %s)", err, v.Pos)
	}
	return shortcutKey{seq: s}, nil
}

// parseSequence reads a fixed sequence written in QML (Shortcut.sequence, hostKeys, modeKey) as
// key.Parse does, but for one spelling QML has always taken: Shift and a character it types that is
// no letter ("Shift+*"). That is the character, with the Shift spent on it, as key.Of reads the key
// press, so it reads as "*". A key-mapping scheme names keys with key.Parse alone, which refuses it.
func parseSequence(s string) (key.Seq, error) {
	seq, err := key.Parse(s)
	if err == nil {
		return seq, nil
	}
	// the parts as key.Parse splits them, each trimmed: " Shift + *" is Shift and *
	if head, rest, ok := strings.Cut(strings.TrimSpace(s), "+"); ok && strings.EqualFold(strings.TrimSpace(head), "shift") {
		if typed, err2 := key.Parse(rest); err2 == nil && typed.Mods == 0 &&
			unicode.IsPrint(typed.Code) && !unicode.IsLetter(typed.Code) {
			return typed, nil
		}
	}
	return key.Seq{}, err
}

// menuShortcut is a MenuItem's shortcut: accelerator text shown as written, or a key command whose
// current key the row shows.
type menuShortcut struct {
	text, command string
}

// menuShortcutOf reads a MenuItem's shortcut: text, or keyId("id").
func menuShortcutOf(v qml.SpecValue) (menuShortcut, error) {
	if r, ok := v.Obj.(keyRef); v.Kind == qml.SpecValueObject && ok {
		return menuShortcut{command: r.id}, nil
	}
	s, err := stringOf(v)
	if err != nil {
		return menuShortcut{}, fmt.Errorf("%w; a key command is written keyId(\"id\")", err)
	}
	return menuShortcut{text: s}, nil
}

// scopeOf reads a keyScope: a scope name.
func scopeOf(v qml.SpecValue) (keymap.Scope, error) {
	s, err := stringOf(v)
	return keymap.Scope(s), err
}

// keymapFile is one Keymap layer.
type keymapFile struct {
	fsys fs.FS
	file string
}

// KeyCommands declares the host's key commands: what keyId names, and what a user's key-mapping scheme
// may rebind. golib's own (widget.KeyCommands) are always declared with them.
func KeyCommands(cmds ...keymap.Command) ProgramOption {
	return func(c *programConfig) { c.keyCommands = append(c.keyCommands, cmds...) }
}

// Keymap adds a key-mapping scheme layer: file in fsys, in Qt Creator's keyboard-mapping-scheme XML
// (keymap.ParseScheme). Layers apply in the order given, a later one winning per command. A file that
// does not exist is an empty layer, so a host may always name a user's file and the user create it
// later; any other read error, a malformed file or one Build refuses fails NewProgram, naming the file
// and line. Under HotReload the files are followed like the layout's.
func Keymap(fsys fs.FS, file string) ProgramOption {
	return func(c *programConfig) { c.keymapFiles = append(c.keymapFiles, keymapFile{fsys, file}) }
}

// loadKeymap is golib's commands and the host's with every Keymap layer over them, as a new Map: a
// reload builds a fresh one, so a refused reload leaves the App's untouched.
func loadKeymap(c programConfig) (*keymap.Map, error) {
	cmds := append(widget.KeyCommands(), c.keyCommands...)
	var schemes []*keymap.Scheme
	for _, f := range c.keymapFiles {
		src, err := fs.ReadFile(f.fsys, f.file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("keymap: reading %s: %w", f.file, err)
		}
		s, err := keymap.ParseScheme(bytes.NewReader(src), f.file)
		if err != nil {
			return nil, err
		}
		schemes = append(schemes, s)
	}
	return keymap.Build(cmds, schemes...)
}

// shortcutHolder is a component that holds Shortcuts (a type in shortcutHolders): the scope of
// its keyId Shortcuts, and the Shortcuts. It is not tui.KeyScoped: a Dialog's or a Drawer's scope
// is its Shortcuts', not one the runtime routes keys by.
type shortcutHolder interface {
	heldShortcuts() (keymap.Scope, []*shortcutNode)
}

// checkShortcuts refuses, each with its position, a keyId Shortcut whose command m does not declare,
// whose holder has no keyScope, or whose command's scope is not its holder's. The reload checks the
// CANDIDATE map before installing anything.
func (a *Adapter) checkShortcuts(m *keymap.Map) error {
	problems := a.shortcutProblems(m)
	if len(problems) == 0 {
		return nil
	}
	return errors.New("keymap: " + strings.Join(problems, "; "))
}

// shortcutProblems are checkShortcuts' refusals, one each, sorted: what CheckKeymap lists beside its
// own.
func (a *Adapter) shortcutProblems(m *keymap.Map) []string {
	var problems []string
	for _, b := range a.nodes {
		h, ok := b.comp.(shortcutHolder)
		if !ok {
			continue
		}
		scope, shortcuts := h.heldShortcuts()
		for _, sc := range shortcuts {
			if sc.key.command == "" {
				continue
			}
			cmd, declared := m.Command(sc.key.command)
			switch {
			case !declared:
				problems = append(problems, fmt.Sprintf("keyId(%q) names no declared key command (at %s)", sc.key.command, sc.pos))
			case scope == "":
				problems = append(problems, fmt.Sprintf("keyId(%q) is in a %s with no keyScope: give it keyScope: %q (at %s)", sc.key.command, b.typ, cmd.Scope, sc.pos))
			case cmd.Scope != scope:
				problems = append(problems, fmt.Sprintf("keyId(%q) is a %s command, in a %s whose scope is %s (at %s)", sc.key.command, cmd.Scope, b.typ, scope, sc.pos))
			}
		}
	}
	slices.Sort(problems)
	return problems
}

// shortcutCommands is each Shortcut as a key command, in order: a keyId's the declared command (its
// keys the map's), a fixed sequence's a command of its own key. ids[i] is shortcuts[i]'s command id.
func shortcutCommands(m *keymap.Map, scope keymap.Scope, shortcuts []*shortcutNode) (cmds []keymap.Command, ids []string) {
	for i, sc := range shortcuts {
		var cmd keymap.Command
		if sc.key.command != "" {
			c, ok := m.Command(sc.key.command)
			if !ok {
				ids = append(ids, "")
				continue
			}
			cmd = c
		} else {
			cmd = keymap.Command{ID: fmt.Sprintf("\x00shortcut:%d", i), Scope: scope, Keys: []key.Seq{sc.key.seq}}
		}
		cmds = append(cmds, cmd)
		ids = append(ids, cmd.ID)
	}
	return cmds, ids
}

// fireShortcut fires the first of shortcuts that ev is, by the one matching model (Context.KeyCommand:
// key.Of, then key.Candidates), and reports whether one was.
func fireShortcut(ctx *tui.Context, scope keymap.Scope, shortcuts []*shortcutNode, ev tui.Event) bool {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind != tui.KeyPress || len(shortcuts) == 0 {
		return false
	}
	cmds, ids := shortcutCommands(ctx.Keymap(), scope, shortcuts)
	cmd, ok := ctx.KeyCommand(k, cmds...)
	if !ok {
		return false
	}
	i := slices.Index(ids, cmd.ID)
	shortcuts[i].trigger()
	return true
}

// shortcutBindings is the holder's Shortcuts as bindings, for App.KeyBindings: a keyId's under its
// command, a fixed sequence's under a command of its own key (an id of "").
func shortcutBindings(m *keymap.Map, scope keymap.Scope, shortcuts []*shortcutNode) []keymap.Binding {
	cmds, _ := shortcutCommands(m, scope, shortcuts)
	out := tui.BindingsOf(m, cmds...)
	for i := range out {
		if strings.HasPrefix(out[i].Command.ID, "\x00shortcut:") {
			out[i].Command.ID = ""
		}
	}
	return out
}
