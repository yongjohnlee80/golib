package decl

import (
	"fmt"
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

// hostKeysOf is the matcher of a comma-separated list of sequences; nil for "".
func hostKeysOf(v qml.SpecValue) (func(tui.KeyEvent) bool, error) { return keysOf("hostKeys", v) }

// modeKeyOf is a Terminal's or TerminalTabs' modeKey: the Shortcut sequences, as hostKeys', that
// toggle input and normal mode (widget.WithModeKey); "" is none.
func modeKeyOf(v qml.SpecValue) (func(tui.KeyEvent) bool, error) { return keysOf("modeKey", v) }

// keysOf is a comma-separated list of Shortcut sequences, as property name takes it: a match for
// any of them, nil for none.
func keysOf(name string, v qml.SpecValue) (func(tui.KeyEvent) bool, error) {
	s, err := stringOf(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
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
	if len(seqs) == 0 {
		return nil, nil
	}
	return func(ev tui.KeyEvent) bool {
		for _, seq := range seqs {
			if seqMatches(seq, ev) {
				return true
			}
		}
		return false
	}, nil
}
