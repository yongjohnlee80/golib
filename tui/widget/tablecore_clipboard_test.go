package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

func press(c *TableCore, keys ...rune) {
	for _, k := range keys {
		c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: k, Text: string(k)})
	}
}

func pressShift(c *TableCore, k rune) {
	c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: k, Mods: tui.ModShift, Text: string(k)})
}

const abc = "h\nA\nB\nC\n"

// TestYankedRowsPasteAsNewRecords: yy then p on A gives A, A, B; P gives the copy above; each one
// undo step; the cursor lands on the first row pasted.
func TestYankedRowsPasteAsNewRecords(t *testing.T) {
	c, _, core, _ := csvTable(abc)
	press(c, 'y', 'y', 'p')
	if want := "h\nA\nA\nB\nC\n"; core.Value() != want {
		t.Fatalf("yy p: %q, want %q", core.Value(), want)
	}
	if c.Cursor().Row != 1 {
		t.Fatalf("yy p: cursor on row %d, want 1 (the row pasted)", c.Cursor().Row)
	}
	undoes(t, core, abc)

	c, _, core, _ = csvTable(abc)
	c.SetCursor(TablePos{2, 0})
	press(c, 'y', 'y')
	pressShift(c, 'P')
	if want := "h\nA\nB\nC\nC\n"; core.Value() != want || c.Cursor().Row != 2 {
		t.Fatalf("yy P on the last row: %q, cursor %d; want %q, the cursor on row 2 (the copy)", core.Value(), c.Cursor().Row, want)
	}

	c, _, core, _ = csvTable(abc)
	pressShift(c, 'V')
	press(c, 'j', 'j', 'y')
	c.SetCursor(TablePos{2, 0})
	press(c, 'p')
	if want := "h\nA\nB\nC\nA\nB\nC\n"; core.Value() != want || c.Cursor().Row != 3 {
		t.Fatalf("three rows pasted after the last: %q, cursor %d; want %q, the cursor on row 3", core.Value(), c.Cursor().Row, want)
	}
}

// TestABlockPastesOverTheCellsAndGrowsOnlyAWholeView: cells overwrite from the cursor's; rows past
// the last are appended in a whole view, cut off (and told once) in a sorted one; fields past the
// last are cut off.
func TestABlockPastesOverTheCellsAndGrowsOnlyAWholeView(t *testing.T) {
	const text = "a,b\n1,2\n3,4\n"
	clipped := 0
	c, _, core, _ := csvTable(text, TableOnPasteClipped(func() { clipped++ }))
	c.SetCursor(TablePos{1, 0})
	if !c.PasteCells([][]string{{"x", "y", "z"}, {"p", "q"}}, TablePasteBlock, false) {
		t.Fatal("refused")
	}
	if want := "a,b\n1,2\nx,y\np,q\n"; core.Value() != want || clipped != 1 {
		t.Fatalf("block: %q (clipped %d), want %q (clipped once: z)", core.Value(), clipped, want)
	}
	undoes(t, core, text)

	clipped = 0
	c, _, core, _ = csvTable(text, TableSortable(true), TableOnPasteClipped(func() { clipped++ }))
	c.SortKeys([]TableSortKey{{Col: 0, Desc: true}}) // 3,4 then 1,2
	c.SetCursor(TablePos{1, 0})
	if !c.PasteCells([][]string{{"x"}, {"past"}}, TablePasteBlock, false) {
		t.Fatal("refused")
	}
	if want := "a,b\nx,2\n3,4\n"; core.Value() != want || clipped != 1 {
		t.Fatalf("sorted: %q (clipped %d), want %q, the shown row's source written and the rest cut off once", core.Value(), clipped, want)
	}
	if c.PasteCells([][]string{{"r"}}, TablePasteRows, false) {
		t.Fatal("rows pasted into a sorted view")
	}
}

// TestASystemPasteIsABlock: a PasteEvent with no field open reads its text and pastes it from the
// cursor; a quoted cell gives its value; a text that does not read pastes nothing.
func TestASystemPasteIsABlock(t *testing.T) {
	const text = "a,b\n1,2\n3,4\n"
	c, _, core, _ := csvTable(text)
	if !c.Paste("x\ty\nz\tw\n") {
		t.Fatal("refused")
	}
	if want := "a,b\nx,y\nz,w\n"; core.Value() != want {
		t.Fatalf("%q, want %q", core.Value(), want)
	}
	c, _, core, _ = csvTable(text)
	if !c.Paste("\"two\nlines\"") || c.Cell(0, 0) != "two\nlines" {
		t.Fatalf("a quoted cell: %q", c.Cell(0, 0))
	}
	c, _, core, _ = csvTable(text)
	if c.Paste("bad\"\tquote\nx") || core.Value() != text {
		t.Fatalf("a text that does not read wrote %q", core.Value())
	}
}

// TestCutYanksThenDeletesOrEmpties: d on a V selection deletes its rows, d on cells empties them,
// x on a V selection empties its rows; the register holds what was cut; one undo each.
func TestCutYanksThenDeletesOrEmpties(t *testing.T) {
	const text = "a,b\n1,2\n3,4\n5,6\n"
	c, _, core, _ := csvTable(text)
	pressShift(c, 'V')
	press(c, 'j', 'd')
	if want := "a,b\n5,6\n"; core.Value() != want {
		t.Fatalf("V j d: %q, want %q", core.Value(), want)
	}
	if reg, rows := c.register().Register(); reg != "1\t2\n3\t4" || !rows {
		t.Fatalf("register %q rows=%v", reg, rows)
	}
	undoes(t, core, text)

	c, _, core, _ = csvTable(text)
	press(c, 'v', 'l', 'd')
	if want := "a,b\n,\n3,4\n5,6\n"; core.Value() != want {
		t.Fatalf("v l d: %q, want %q", core.Value(), want)
	}

	c, _, core, _ = csvTable(text)
	pressShift(c, 'V')
	press(c, 'x')
	if want := "a,b\n,\n3,4\n5,6\n"; core.Value() != want {
		t.Fatalf("V x: %q, want %q (emptied, not deleted)", core.Value(), want)
	}
}

// TestACutPastTheCopyLimitDeletesNothing: the yank is refused, so nothing is cut.
func TestACutPastTheCopyLimitDeletesNothing(t *testing.T) {
	const text = "a,b\nlonger than four,2\n"
	refused := 0
	c, _, core, _ := csvTable(text, TableCopyLimit(4), TableOnCopyRefused(func(int) { refused++ }))
	press(c, 'v', 'd')
	if core.Value() != text || refused != 1 {
		t.Fatalf("a refused yank cut: %q (refused %d)", core.Value(), refused)
	}
}

// TestTheRegisterIsTheHosts: a yank goes to the store given, and a paste reads it: a register
// shared by two tables carries a yank from one to the other.
func TestTheRegisterIsTheHosts(t *testing.T) {
	shared := &ownRegister{}
	a, _, _, _ := csvTable("h\nfrom a\n", TableRegister(shared))
	b, _, coreB, _ := csvTable("h\nb\n", TableRegister(shared))
	press(a, 'y', 'y')
	press(b, 'p')
	if want := "h\nb\nfrom a\n"; coreB.Value() != want {
		t.Fatalf("%q, want %q", coreB.Value(), want)
	}
}

// TestDWithASelectionCutsOnceAndDdStill: d on a selection cuts at once and leaves no d waiting;
// without one, d waits for its second d; a movement between drops it.
func TestDWithASelectionCutsOnceAndDdStill(t *testing.T) {
	const text = "a\n1\n2\n3\n"
	c, _, core, _ := csvTable(text)
	press(c, 'v', 'd', 'd')
	if want := "a\n\"\"\n2\n3\n"; core.Value() != want { // the cell emptied (a lone empty field is ""); no row deleted
		t.Fatalf("v d d: %q, want %q: the selection cut once, then the second d waits", core.Value(), want)
	}
	c, _, core, _ = csvTable(text)
	press(c, 'd', 'j', 'd')
	if core.Value() != text {
		t.Fatalf("d j d deleted: %q", core.Value())
	}
	press(c, 'd', 'd')
	if want := "a\n1\n3\n"; core.Value() != want {
		t.Fatalf("dd: %q, want %q", core.Value(), want)
	}
}

// TestInsertAndDeleteRowsAndFields: the menu's commands, at the first and the last, one step each;
// the last field is refused.
func TestInsertAndDeleteRowsAndFields(t *testing.T) {
	const text = "a,b\n1,2\n3,4\n"
	c, _, core, _ := csvTable(text)
	if !c.InsertRows(true) || core.Value() != "a,b\n,\n1,2\n3,4\n" {
		t.Fatalf("a row above the first: %q", core.Value())
	}
	c, _, core, _ = csvTable(text)
	c.SetCursor(TablePos{1, 1})
	if !c.InsertFields(false) || core.Value() != "a,b,\n1,2,\n3,4,\n" {
		t.Fatalf("a field right of the last: %q", core.Value())
	}
	c, _, core, _ = csvTable(text)
	c.SetCursor(TablePos{1, 1})
	if !c.DeleteFields() || core.Value() != "a\n1\n3\n" {
		t.Fatalf("the last field deleted: %q", core.Value())
	}
	if c.DeleteFields() {
		t.Fatalf("the only field deleted: %q", core.Value())
	}
	c, _, core, _ = csvTable(text)
	pressShift(c, 'V')
	press(c, 'j')
	if !c.DeleteRows() || core.Value() != "a,b\n" {
		t.Fatalf("both rows deleted: %q", core.Value())
	}
	undoes(t, core, text)
}

// TestTheDocumentsMenu: its rows run on the table, show their keys' commands, and are disabled on
// a read-only document; Row numbers toggles the gutter.
func TestTheDocumentsMenu(t *testing.T) {
	ed := NewEditorCore(CoreInitialText("a,b\n1,2\n"))
	d := NewTableDocument(ed, ',')
	rows := d.menuRows()
	byID := map[ItemID]MenuItemModel{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if r := byID["table.yank"]; !r.Enabled || r.AccelCommand != TableYank.ID {
		t.Fatalf("yank row: %+v", r)
	}
	if r := byID["table.paste"]; r.Enabled {
		t.Fatal("paste enabled with an empty register")
	}
	numbers := byID["table.rowNumbers"]
	numbers.Action.(CoreMenuAction).Run(nil)
	if !d.Core().RowNumbers() {
		t.Fatal("Row numbers did not turn the gutter on")
	}
	ro := NewEditorCore(CoreInitialText("a,b\n1,2\n"))
	ro.SetReadOnly(true)
	for _, r := range NewTableDocument(ro, ',').menuRows() {
		if r.Enabled && (r.ID == "table.cut" || r.ID == "table.rowBelow" || r.ID == "table.deleteRows") {
			t.Fatalf("%s enabled on a read-only document", r.ID)
		}
	}
	// the document's register is the editor's
	byID["table.yank"].Action.(CoreMenuAction).Run(nil)
	if text, _ := ed.Register(); text != "1" {
		t.Fatalf("the editor's register holds %q after the table's yank", text)
	}
}

// TestAnEmptyCellYankedPastesAsEmpty: y on an empty cell, p over another, empties it.
func TestAnEmptyCellYankedPastesAsEmpty(t *testing.T) {
	c, _, core, _ := csvTable("a,b\n,x\n")
	press(c, 'y')
	c.SetCursor(TablePos{0, 1})
	press(c, 'p')
	if want := "a,b\n,\n"; core.Value() != want {
		t.Fatalf("%q, want %q", core.Value(), want)
	}
}

// TestRowsPastedOnTheEdgeRowAppend: on the empty row after the last (TableAppendEdges), p and P
// append the register's rows.
func TestRowsPastedOnTheEdgeRowAppend(t *testing.T) {
	for _, before := range []bool{false, true} {
		c, _, core, _ := csvTable(abc, TableAppendEdges())
		press(c, 'y', 'y')
		c.SetCursor(TablePos{c.Len(), 0})
		if c.Cursor().Row != c.Len() {
			t.Fatalf("the cursor did not reach the edge row: %+v", c.Cursor())
		}
		if !c.PasteRegister(before) {
			t.Fatalf("P=%v on the edge row refused", before)
		}
		if want := "h\nA\nB\nC\nA\n"; core.Value() != want || c.Cursor().Row != 3 {
			t.Fatalf("P=%v: %q, cursor %d; want %q, the cursor on row 3 (the row pasted)", before, core.Value(), c.Cursor().Row, want)
		}
	}
}

// TestAnEmptyCellThroughTheMenu: the menu's Yank on an empty cell, then its Paste over a full one,
// empties it, and Paste is enabled for that register.
func TestAnEmptyCellThroughTheMenu(t *testing.T) {
	ed := NewEditorCore(CoreInitialText("h\n\"\"\nvalue\n"))
	d := NewTableDocument(ed, ',')
	run := func(id ItemID) {
		for _, r := range d.menuRows() {
			if r.ID == id {
				if !r.Enabled {
					t.Fatalf("%s disabled", id)
				}
				r.Action.(CoreMenuAction).Run(nil)
				return
			}
		}
		t.Fatalf("no %s row", id)
	}
	run("table.yank")
	d.Core().SetCursor(TablePos{1, 0})
	run("table.paste")
	if want := "h\n\"\"\n\"\"\n"; ed.Value() != want {
		t.Fatalf("%q, want %q", ed.Value(), want)
	}
}

// TestRowsGoInOnTheEdgeRowAndAHeaderOnlyTable: Insert row below on the edge row appends; p on a
// table with no records yet appends; the cursor lands on the new row.
func TestRowsGoInOnTheEdgeRowAndAHeaderOnlyTable(t *testing.T) {
	c, _, core, _ := csvTable("h\nA\n", TableAppendEdges())
	c.SetCursor(TablePos{c.Len(), 0})
	if !c.InsertRows(false) || core.Value() != "h\nA\n\"\"\n" || c.Cursor().Row != 1 {
		t.Fatalf("Insert row below on the edge row: %q, cursor %d; want the cursor on row 1", core.Value(), c.Cursor().Row)
	}
	for _, edges := range []bool{false, true} {
		opts := []TableOption{TableRegister(&ownRegister{text: "B\nC", linewise: true})}
		if edges {
			opts = append(opts, TableAppendEdges())
		}
		c, _, core, _ = csvTable("h\n", opts...)
		if !c.PasteRegister(false) || core.Value() != "h\nB\nC\n" || c.Cursor().Row != 0 {
			t.Fatalf("p on a header-only table (edges %v): %q, cursor %d; want the cursor on row 0", edges, core.Value(), c.Cursor().Row)
		}
		undoes(t, core, "h\n")
	}
}

// TestACellNamedTwiceRefusesTheOp: nothing written, the revision and the undo history as they
// were; repeated insert positions stay allowed.
func TestACellNamedTwiceRefusesTheOp(t *testing.T) {
	const text = "h\nA\n"
	src, c := csvOn(text, ',')
	rev := c.Revision()
	if src.CommitCells(TableCellsOp{Cells: []TableCell{{0, 0, "B"}, {0, 0, "C"}}}) ||
		src.CommitCells(TableCellsOp{Cells: []TableCell{{-1, 0, "x"}, {-1, 0, "y"}}}) {
		t.Fatal("a cell named twice was accepted")
	}
	if c.Value() != text || c.Revision() != rev {
		t.Fatalf("wrote %q", c.Value())
	}
	c.Undo()
	if c.Value() != text {
		t.Fatalf("an undo step was left: %q", c.Value())
	}
	if !src.CommitCells(TableCellsOp{InsertRows: []int{1, 1}, Cells: []TableCell{{1, 0, "x"}, {2, 0, "y"}}}) ||
		c.Value() != "h\nA\nx\ny\n" {
		t.Fatalf("repeated insert positions: %q", c.Value())
	}
}

// TestTheMenuDisablesWhatCannotRun: a linewise register in a sorted view, and a selection of every
// field, disable Paste and Delete fields.
func TestTheMenuDisablesWhatCannotRun(t *testing.T) {
	ed := NewEditorCore(CoreInitialText("a,b\n1,2\n3,4\n"))
	d := NewTableDocument(ed, ',', TableSortable(true))
	c := d.Core()
	press(c, 'y', 'y')
	c.SortKeys([]TableSortKey{{Col: 0, Desc: true}})
	press(c, 'v', '$') // every field selected (a V selection is rows: Delete fields acts on the cursor's)
	enabled := map[ItemID]bool{}
	for _, r := range d.menuRows() {
		enabled[r.ID] = r.Enabled
	}
	if enabled["table.paste"] || enabled["table.deleteFields"] {
		t.Fatalf("paste %v, delete fields %v: want both disabled", enabled["table.paste"], enabled["table.deleteFields"])
	}
}

// TestEveryMenuRowRuns: each of the document's rows runs its command on the table, and the host's
// rows follow the table's own.
func TestEveryMenuRowRuns(t *testing.T) {
	const text = "a,b\n1,2\n3,4\n"
	for _, tc := range []struct {
		id   ItemID
		prep func(c *TableCore)
		want string
	}{
		{"table.cut", nil, "a,b\n,2\n3,4\n"},
		{"table.paste", func(c *TableCore) { c.register().SetRegister("x", false) }, "a,b\nx,2\n3,4\n"},
		{"table.rowAbove", nil, "a,b\n,\n1,2\n3,4\n"},
		{"table.rowBelow", nil, "a,b\n1,2\n,\n3,4\n"},
		{"table.fieldLeft", nil, ",a,b\n,1,2\n,3,4\n"},
		{"table.fieldRight", nil, "a,,b\n1,,2\n3,,4\n"},
		{"table.deleteRows", nil, "a,b\n3,4\n"},
		{"table.deleteFields", nil, "b\n2\n4\n"},
	} {
		ed := NewEditorCore(CoreInitialText(text))
		d := NewTableDocument(ed, ',')
		if tc.prep != nil {
			tc.prep(d.Core())
		}
		ran := false
		for _, r := range d.menuRows() {
			if r.ID == tc.id {
				r.Action.(CoreMenuAction).Run(nil)
				ran = true
			}
		}
		if !ran || ed.Value() != tc.want {
			t.Fatalf("%s: ran %v, %q; want %q", tc.id, ran, ed.Value(), tc.want)
		}
	}
	ed := NewEditorCore(CoreInitialText(text))
	d := NewTableDocument(ed, ',')
	for _, r := range d.menuRows() {
		if r.ID == "table.selectAll" {
			r.Action.(CoreMenuAction).Run(nil)
		}
	}
	if kind := d.Core().SelectionKind(); kind != TableSelectRows {
		t.Fatalf("Select all: %v, want every row", kind)
	}
	d.SetMenuRows(func() []MenuItemModel {
		return []MenuItemModel{NewCommand("host.row", "Host", CoreMenuAction{ID: "host.row"})}
	})
	rows := d.menuRows()
	if last := rows[len(rows)-1]; last.ID != "host.row" || rows[len(rows)-2].Kind != ItemKindSeparator {
		t.Fatalf("the host's rows: last %q after %v", last.ID, rows[len(rows)-2].Kind)
	}
}

// TestARightPressSelectsWhatItWasOn: on a row number outside the selection, that row; on a title
// outside it, that field; inside the selection, nothing changes; on an empty table, nothing.
func TestARightPressSelectsWhatItWasOn(t *testing.T) {
	c := NewTableCore(people(), TableMenuSelects(true))
	c.Bind(nil, &fakeTableLayout{})
	c.selectForMenu(TablePos{Row: 2}, TableHitGutter)
	if from, to, ok := c.Selection(); !ok || c.SelectionKind() != TableSelectRows || from.Row != 2 || to.Row != 2 {
		t.Fatalf("a row number: %+v-%+v %v %v", from, to, ok, c.SelectionKind())
	}
	c.selectForMenu(TablePos{Row: 2}, TableHitGutter) // inside: kept
	if !c.rowSelected(2) {
		t.Fatal("a second press on the row's number dropped it")
	}
	c.selectForMenu(TablePos{Col: 1}, TableHitHeader)
	if !c.fieldSelected(1) || c.fieldSelected(0) {
		t.Fatal("a title did not select its field alone")
	}
	c.selectForMenu(TablePos{Row: 1, Col: 1}, TableHitCell) // inside the field: kept
	if !c.fieldSelected(1) {
		t.Fatal("a press inside the field's selection dropped it")
	}
	c.selectForMenu(TablePos{Row: 0, Col: 0}, TableHitCell)
	if _, _, ok := c.Selection(); ok || c.Cursor() != (TablePos{0, 0}) {
		t.Fatalf("a cell outside: selection %v, cursor %+v", ok, c.Cursor())
	}
	if c.Dragging() {
		t.Fatal("a right press started a drag")
	}
	empty, _, _, _ := csvTable("a,b\n", TableMenuSelects(true))
	empty.selectForMenu(TablePos{}, TableHitGutter)
	if _, _, ok := empty.Selection(); ok {
		t.Fatal("an empty table selected a row")
	}
	empty.selectForMenu(TablePos{Col: 1}, TableHitHeader)
	if _, _, ok := empty.Selection(); ok || empty.Cursor().Col != 1 {
		t.Fatalf("a title on an empty table: selection %v, cursor %+v; want none, on the field", ok, empty.Cursor())
	}
	press(empty, 'y', 'y')
	if text, _ := empty.register().Register(); text != "" {
		t.Fatalf("yy on an empty table yanked %q", text)
	}
}

// TestByDefaultARightPressKeepsTheSelection: without TableMenuSelects, a right press anywhere
// leaves the selection and the cursor as they were (a results table's menu acts on them).
func TestByDefaultARightPressKeepsTheSelection(t *testing.T) {
	c := NewTableCore(people())
	c.Bind(nil, &fakeTableLayout{})
	c.Select(TableSelectRows, TablePos{})
	c.SetCursor(TablePos{Row: 1})
	for _, hit := range []TableHit{TableHitCell, TableHitGutter, TableHitHeader} {
		c.selectForMenu(TablePos{Row: 3, Col: 2}, hit)
		if from, to, ok := c.Selection(); !ok || from.Row != 0 || to.Row != 1 || c.Cursor() != (TablePos{Row: 1}) {
			t.Fatalf("hit %v: selection %v %+v-%+v, cursor %+v; want rows 0-1 kept", hit, ok, from, to, c.Cursor())
		}
	}
}

// TestAReadOnlyTableCopiesItsValuesAsTheyAre: a read-only table's copy is its values joined by
// tabs, a quote or an empty cell as it is; an editable table's is quoted TSV, to paste back.
func TestAReadOnlyTableCopiesItsValuesAsTheyAre(t *testing.T) {
	ro := NewTableCore(&rowsSource{fields: []TableField{{Title: "j"}, {Title: "e"}}, rows: [][]string{{`{"a": 1}`, ""}}})
	ro.Bind(nil, &fakeTableLayout{})
	press(ro, 'y', 'y')
	if got := ro.CopyText(); got != `{"a": 1}` {
		t.Fatalf("a read-only cell copied %q", got)
	}
	if text, _ := ro.register().Register(); text != "{\"a\": 1}\t" {
		t.Fatalf("a read-only row copied %q", text)
	}
	ed, _, _, _ := csvTable("j,e\n\"{\"\"a\"\": 1}\",\n")
	if got := ed.CopyText(); got != `"{""a"": 1}"` {
		t.Fatalf("an editable cell copied %q", got)
	}
}

// A CSV table's register reads back as CSV: yy then p on a row whose cell holds a comma pastes the
// same two cells, where reading it as TSV would make one cell of the whole line.
func TestACSVTablesRegisterPastesBackAsCSV(t *testing.T) {
	text := "a,b\n\"x,1\",y\n"
	c, _, core, _ := csvTable(text, TableCopyAs(TableCopyCSV))
	press(c, 'y', 'y', 'p')
	if want := "a,b\n\"x,1\",y\n\"x,1\",y\n"; core.Value() != want {
		t.Fatalf("yy p in a CSV table: %q, want %q", core.Value(), want)
	}
	if reg, rows := c.register().Register(); reg != `"x,1",y` || !rows {
		t.Fatalf("the register %q (rows %v), want the row as CSV", reg, rows)
	}
}

// A row cursor's copy is its row, whole and linewise, by every path: y, Copy, and the document
// menu's Yank; a selection made on a row cursor is still what is copied.
func TestARowCursorsEveryCopyIsTheRow(t *testing.T) {
	ed := NewEditorCore(CoreInitialText("a,b\n1,2\n3,4\n"))
	d := NewTableDocument(ed, ',')
	c := d.Core()
	c.SetCursor(TablePos{1, 1})
	c.SetRowCursor(true)
	for name, copyNow := range map[string]func(){
		"y":    func() { press(c, 'y') },
		"Copy": func() { c.Copy() },
		"the menu's Yank": func() {
			for _, r := range d.menuRows() {
				if r.ID == "table.yank" {
					r.Action.(CoreMenuAction).Run(nil)
				}
			}
		},
	} {
		ed.SetRegister("", false)
		c.yanked = false
		copyNow()
		if text, rows := c.register().Register(); text != "3\t4" || !rows {
			t.Errorf("%s on a row cursor: %q (rows %v), want row 1 whole, linewise", name, text, rows)
		}
	}
	c.SetAnchor(&TablePos{0, 1}) // a selection on the row cursor: field 1 of rows 0 and 1
	c.kind = TableSelectBlock
	c.Copy()
	if text, rows := c.register().Register(); text != "2\n4" || rows {
		t.Errorf("a block selected on a row cursor copied %q (rows %v), want its cells", text, rows)
	}
}

// The copy format names itself, defaults to TSV, and is set and read back; the row cursor reads
// back and setting it to what it is changes nothing.
func TestTheCopyFormatAndTheRowCursorReadBack(t *testing.T) {
	for f, want := range map[TableCopyFormat]string{TableCopyTSV: "tsv", TableCopyCSV: "csv", TableCopyFormat(9): "unknown"} {
		if got := f.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", f, got, want)
		}
	}
	c := NewTableCore(people())
	if c.CopyFormat() != TableCopyTSV || c.RowCursor() {
		t.Fatalf("a new core copies %v, row cursor %v; want tsv, a cell cursor", c.CopyFormat(), c.RowCursor())
	}
	c.SetCopyFormat(TableCopyCSV)
	c.SetRowCursor(true)
	c.SetRowCursor(true)
	if c.CopyFormat() != TableCopyCSV || !c.RowCursor() {
		t.Fatalf("set: copies %v, row cursor %v", c.CopyFormat(), c.RowCursor())
	}
	if TableCopyAs(TableCopyCSV)(c); c.copyFormat != TableCopyCSV {
		t.Fatal("TableCopyAs did not set the format")
	}
}
