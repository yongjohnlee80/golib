package widget

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// THE EDITOR'S CROSS-VERSION DIFFERENTIAL (ADR 1792255248 §5.1). The Editor's input code is too large
// to keep a verbatim copy of as an oracle, so this cell RECORDS: it drives the grid through EditorCore
// from a dozen starting states using only API that golib 6a0b271 already had, and writes one line per
// (state, key) to $EDITOR_GRID_OUT. The gate runs it in a 6a0b271 tree and in the branch's, and
// compares the two files; every difference must be a documented deviation. Without the variable it
// only checks that the recording runs.

// gridState is a starting state, reached by keys every version reads alike.
type gridState struct {
	name  string
	opts  []CoreOption
	setup []tui.KeyEvent
	hook  bool // an old-style host chord hook, recording what it is shown
}

func typed(s string) []tui.KeyEvent {
	var out []tui.KeyEvent
	for _, r := range s {
		out = append(out, tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)})
	}
	return out
}

var gridEditorText = CoreInitialText("hello big world\nsecond line here\nthird")

var editorGridStates = []gridState{
	{"vim normal", []CoreOption{gridEditorText}, typed("w"), false},
	{"vim normal, a count", []CoreOption{gridEditorText}, typed("w2"), false},
	{"vim normal, d pending", []CoreOption{gridEditorText}, typed("wd"), false},
	{"vim normal, g pending", []CoreOption{gridEditorText}, typed("wg"), false},
	{"vim normal, f armed", []CoreOption{gridEditorText}, typed("wf"), false},
	{"vim visual", []CoreOption{gridEditorText}, typed("wvl"), false},
	{"vim insert", []CoreOption{gridEditorText}, typed("wi"), false},
	{"vim normal, a host hook", []CoreOption{gridEditorText}, typed("w"), true},
	{"vim normal, read-only", []CoreOption{gridEditorText, CoreReadOnly(true)}, typed("w"), false},
	{"standard", []CoreOption{gridEditorText, CoreKeyset(KeysetStandard)}, nil, false},
	{"nano", []CoreOption{gridEditorText, CoreKeyset(KeysetNano)}, nil, false},
	{"standard, a selection", []CoreOption{gridEditorText, CoreKeyset(KeysetStandard)},
		[]tui.KeyEvent{{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModShift}, {Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModShift}}, false},
}

// editorGridLine is one key's effect from one state, every observable the old API has.
func editorGridLine(st gridState, e tui.KeyEvent) string {
	c := NewEditorCore(st.opts...)
	var hooked []string
	if st.hook {
		c.SetChordHook(func(kc KeyChord) bool {
			hooked = append(hooked, fmt.Sprintf("%q/%v", kc.Code, kc.Ctrl))
			return kc.Code == 'x' && !kc.Ctrl // the host takes x
		})
	}
	for _, k := range st.setup {
		c.HandleKey(k)
	}
	ret := c.HandleKey(e)
	ln, col := c.Line()
	var sel []string
	for l := 0; l < len(c.Lines()); l++ {
		for x := 0; x <= len([]rune(c.Lines()[l])); x++ {
			if c.Selected(l, x) {
				sel = append(sel, fmt.Sprintf("%d:%d", l, x))
			}
		}
	}
	return fmt.Sprintf("ret=%v mode=%v at=%d:%d undo=%v sel=%s hook=%s value=%q",
		ret, c.Mode(), ln, col, c.CanUndo(), strings.Join(sel, ","), strings.Join(hooked, ","), c.Value())
}

func TestEditorKeyGridRecord(t *testing.T) {
	var b strings.Builder
	for _, st := range editorGridStates {
		for _, e := range keyGrid() {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", st.name, describeKey(e), editorGridLine(st, e))
		}
	}
	out := os.Getenv("EDITOR_GRID_OUT")
	if out == "" {
		t.Logf("recorded %d bytes; set EDITOR_GRID_OUT to keep them", b.Len())
		return
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
