package widget_test

// Table contract: header row + column-aligned rows over a List core.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

type crewRow struct{ name, role, note string }

func crewColumns() []widget.TableColumn[crewRow] {
	return []widget.TableColumn[crewRow]{
		{Title: "Name", Width: 10, Cell: func(r crewRow) string { return r.name }},
		{Title: "Role", Width: 8, Cell: func(r crewRow) string { return r.role }},
		{Title: "Note", Width: 0, Cell: func(r crewRow) string { return r.note }}, // flex
	}
}

func TestTableHeaderAndRows(t *testing.T) {
	tab := widget.NewTable(crewColumns())
	sh := newShell(tab)
	h := startApp(t, sh, 48, 6)
	h.onLoop(func() {
		tab.SetItems([]crewRow{
			{"ada", "eng", "first"},
			{"grace", "capt", "cobol"},
		})
	})
	h.barrier(sh)

	h.wantContains("Name")
	h.wantContains("Role")
	h.wantContains("Note")
	h.wantContains("ada")
	h.wantContains("cobol")

	// Header and cells align: each row's "Role" column starts where the
	// header says it does (fixed 10 + 2-gap layout).
	scr := h.grid()
	lines := strings.Split(scr, "\n")
	hdr, row := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "Name") {
			hdr = i
		}
		if strings.Contains(l, "ada") {
			row = i
		}
	}
	if hdr < 0 || row < 0 {
		t.Fatalf("header/row not found:\n%s", scr)
	}
	if strings.Index(lines[hdr], "Role") != strings.Index(lines[row], "eng") {
		t.Errorf("Role column misaligned:\n%q\n%q", lines[hdr], lines[row])
	}
}

func TestTableEmptyText(t *testing.T) {
	tab := widget.NewTable(crewColumns(), widget.WithEmptyText[crewRow]("no crew yet"))
	sh := newShell(tab)
	h := startApp(t, sh, 48, 6)
	h.barrier(sh)
	h.wantContains("Name") // header always renders
	h.wantContains("no crew yet")
}

func TestTableCursorNavigation(t *testing.T) {
	tab := widget.NewTable(crewColumns())
	sh := newShell(tab)
	h := startApp(t, sh, 48, 6)
	h.onLoop(func() {
		tab.SetItems([]crewRow{{"ada", "eng", ""}, {"grace", "capt", ""}})
	})
	h.barrier(sh)

	var idx int
	var ok bool
	h.onLoop(func() { idx, ok = tab.Selected() })
	if !ok || idx != 0 {
		t.Fatalf("initial Selected() = %d,%v; want 0,true", idx, ok)
	}
	// Down moves the cursor even when the event is delivered to the Table
	// (controller-forwarding path).
	h.onLoop(func() { tab.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}) })
	h.onLoop(func() { idx, ok = tab.Selected() })
	if !ok || idx != 1 {
		t.Errorf("after Down Selected() = %d,%v; want 1,true", idx, ok)
	}
}
