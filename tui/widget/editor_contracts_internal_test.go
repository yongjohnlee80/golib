package widget

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// A menu with no owner has no Context to open over: OpenAt refuses, rather than reaching through nil.
func TestAMenuWithNoOwnerDoesNotOpen(t *testing.T) {
	m := NewEditorMenu(nil, nil)
	m.SetEnabled(true)
	m.SetRows(func() []MenuItemModel { return []MenuItemModel{NewCommand("x", "X", CoreMenuAction{ID: "x"})} })
	if OwnerContext(nil) != nil || m.OpenAt(tui.Point{}) {
		t.Error("a menu with no owner opened")
	}
}

// A chord names a key no name covers by its code; Alt and Super lead as Ctrl does. An Action
// past the table is a custom one, bound in no class.
func TestKeyChordAndActionNameTheirEdges(t *testing.T) {
	if s := (KeyChord{Code: 0xE123, Alt: true, Super: true}).String(); !strings.HasPrefix(s, "Alt+Super+") || !strings.Contains(s, "Key(0xe123)") {
		t.Errorf("chord named %q", s)
	}
	custom := actMax + 3
	if custom.String() != "Action("+strconv.Itoa(int(custom))+")" || custom.Description() != "Custom action" {
		t.Errorf("a custom action: %q, %q", custom.String(), custom.Description())
	}
	if n, v, i := actionModes(custom); n || v || i {
		t.Error("a custom action is bound in a class")
	}
	for m, want := range map[EditorMode]EditorMode{ModeVisualLine: ModeVisual, ModeVisualBlock: ModeVisual, ModeReplace: ModeInsert, ModeNormal: ModeNormal} {
		if got := modeClass(m); got != want {
			t.Errorf("modeClass(%v) = %v, want %v", m, got, want)
		}
	}
}

// A read-only core leaves Insert for Normal; an indent unit that is not whitespace is refused, at
// construction by a panic and at run time by an error that keeps the old unit.
func TestAnEditorCoresReadOnlyAndIndentUnit(t *testing.T) {
	c := NewEditorCore()
	c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: 'i', Text: "i"})
	c.SetReadOnly(true)
	if c.Mode() != ModeNormal {
		t.Errorf("a read-only core stayed in %v", c.Mode())
	}
	c.SetAutoIndent(true)
	if err := c.SetIndentUnit("  "); err != nil {
		t.Fatal(err)
	}
	var unitErr *IndentUnitError
	if err := c.SetIndentUnit("x"); err == nil || c.indentUnit != "  " || !errors.As(err, &unitErr) || unitErr.Error() == "" {
		t.Errorf("an indent unit of x: %v, unit now %q", err, c.indentUnit)
	}
	if !widgettest.Panics(func() { CoreIndentUnit("x") }) {
		t.Error("CoreIndentUnit took x")
	}
}

// A chosen row runs as its action says: a CoreMenuAction on the menu's core, an EditorMenuAction on
// a tui Editor that owns the menu. An EditorMenuAction under another owner, an action with no Run,
// and an action the menu does not know run nothing.
func TestAnEditorMenuRunsEachRowAsItsActionSays(t *testing.T) {
	ed := NewEditor()
	var ranCore *EditorCore
	var ranEditor *Editor
	coreAct := CoreMenuAction{ID: "c", Run: func(c *EditorCore) { ranCore = c }}
	edAct := EditorMenuAction{ID: "e", Run: func(e *Editor) { ranEditor = e }}

	m := NewEditorMenu(ed, ed.core)
	if run := m.action(coreAct); run == nil {
		t.Fatal("a CoreMenuAction does not run")
	} else if run(); ranCore != ed.core {
		t.Error("a CoreMenuAction did not run on the menu's core")
	}
	if run := m.action(edAct); run == nil {
		t.Fatal("an EditorMenuAction does not run in its Editor's menu")
	} else if run(); ranEditor != ed {
		t.Error("an EditorMenuAction did not run on the menu's Editor")
	}

	other := NewEditorMenu(NewTextInput(), ed.core)
	for name, a := range map[string]tui.Action{
		"an EditorMenuAction under another owner": edAct,
		"a CoreMenuAction with no Run":            CoreMenuAction{ID: "n"},
		"an EditorMenuAction with no Run":         EditorMenuAction{ID: "n"},
		"an action the menu does not know":        tui.ActivateAction{},
	} {
		if other.action(a) != nil {
			t.Errorf("%s runs", name)
		}
	}
}
