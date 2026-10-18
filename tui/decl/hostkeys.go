package decl

import (
	"fmt"
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/key"
)

// HOST KEYS — a Terminal's (or TerminalTabs') keys its program never sees:
//
//	TerminalTabs { hostKeys: "F11, Ctrl+Shift+M" }
//
// Each is a sequence as a Shortcut writes one; "" is none. They go on to the Window's Shortcuts
// instead, so a key the host binds works with the keyboard in a shell.
//
// modeKey takes the same list: the keys that toggle the terminal's input and normal mode
// (widget.WithModeKey), "Ctrl+T", say.
//
// A comma is the list's separator, so a list written as text cannot hold the comma key. A host that
// follows a key map binds either property to a Keys source instead: its sequences, never split.

// Keys are keys a document holds as one value, for hostKeys and modeKey: each sequence is one key,
// the comma's too. A host publishes a command's keys as the map binds them (keymap.Map.KeysOf);
// empty is none.
type Keys []key.Seq

// hostKeysOf is the matcher of a comma-separated list of sequences; nil for "".
func hostKeysOf(v qml.SpecValue) (func(tui.KeyEvent) bool, error) { return keysOf("hostKeys", v) }

// modeKeyOf is a Terminal's or TerminalTabs' modeKey: the Shortcut sequences, as hostKeys', that
// toggle input and normal mode (widget.WithModeKey); "" is none.
func modeKeyOf(v qml.SpecValue) (func(tui.KeyEvent) bool, error) { return keysOf("modeKey", v) }

// keysOf is a comma-separated list of Shortcut sequences, as property name takes it: a match for
// any of them, nil for none.
func keysOf(name string, v qml.SpecValue) (func(tui.KeyEvent) bool, error) {
	if ks, ok := v.Obj.(Keys); v.Kind == qml.SpecValueObject && ok {
		return matcherOf(ks), nil
	}
	s, err := stringOf(v)
	if err != nil {
		return nil, fmt.Errorf("%s: want a comma-separated list of sequences or a Keys value: %w", name, err)
	}
	var seqs []key.Seq
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		seq, err := parseSequence(part)
		if err != nil {
			return nil, fmt.Errorf("%s: %w (at %s)", name, err, v.Pos)
		}
		seqs = append(seqs, seq)
	}
	return matcherOf(seqs), nil
}

// matcherOf is a match for any of seqs, nil for none.
func matcherOf(seqs []key.Seq) func(tui.KeyEvent) bool {
	if len(seqs) == 0 {
		return nil
	}
	seqs = slices.Clone(seqs)
	return func(ev tui.KeyEvent) bool {
		for _, seq := range seqs {
			if seqMatches(seq, ev) {
				return true
			}
		}
		return false
	}
}
