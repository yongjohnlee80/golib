package decl

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

// HOST KEYS — a Terminal's (or TerminalTabs') keys its program never sees:
//
//	TerminalTabs { hostKeys: "F11, Ctrl+Shift+M" }
//
// Each is a sequence as a Shortcut writes one; "" is none. They go on to the Window's Shortcuts
// instead, so a key the host binds works with the keyboard in a shell.

// hostKeysOf is the matcher of a comma-separated list of sequences; nil for "".
func hostKeysOf(v qml.SpecValue) (func(tui.KeyEvent) bool, error) {
	s, err := stringOf(v)
	if err != nil {
		return nil, fmt.Errorf("hostKeys: %w", err)
	}
	var seqs []keySequence
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		seq, err := parseSequence(part)
		if err != nil {
			return nil, fmt.Errorf("hostKeys: %w (at %s)", err, v.Pos)
		}
		seqs = append(seqs, seq)
	}
	if len(seqs) == 0 {
		return nil, nil
	}
	return func(ev tui.KeyEvent) bool {
		for _, seq := range seqs {
			if seq.matches(ev) {
				return true
			}
		}
		return false
	}, nil
}
