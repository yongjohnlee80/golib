package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// csvTable is a TableCore editing a CSV document, with a fake layout, and how many edits it was
// told were dropped.
func csvTable(text string, opts ...TableOption) (*TableCore, *TableCSV, *EditorCore, *int) {
	src, core := csvOn(text, ',')
	dropped := new(int)
	c := NewTableCore(src, append([]TableOption{TableOnEditDropped(func() { *dropped++ })}, opts...)...)
	c.Bind(nil, &fakeTableLayout{})
	return c, src, core, dropped
}

func typeInto(c *TableCore, s string) {
	for _, r := range s {
		c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)})
	}
}

var (
	altEnter = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter, Mods: tui.ModAlt}
	tabKey   = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}
	escKey   = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}
	enterKey = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
)

// TestATableCellIsEditedInItsField: Enter opens the field on the cell's exact value; Enter
// kept untouched writes nothing and moves down; an edit is written as one undo step; Alt+Enter and
// Ctrl+V Tab put a newline and a tab in the value; Esc drops; i, F2 and a typed key open it too.
func TestATableCellIsEditedInItsField(t *testing.T) {
	const text = "h,v\na,\"one\ntwo\"\nb,2\n"
	c, src, core, _ := csvTable(text)
	c.SetCursor(TablePos{0, 1})
	c.HandleKey(enterKey)
	e := c.Editing()
	if e == nil || e.Field.Value() != "one\ntwo" || e.Pos != (TablePos{0, 1}) {
		t.Fatalf("Enter on a cell: %+v", e)
	}
	rev := core.Revision()
	c.HandleKey(enterKey)
	if c.Editing() != nil || core.Revision() != rev || c.Cursor() != (TablePos{1, 1}) {
		t.Fatalf("an untouched keep: editing %v, wrote %v, cursor %v", c.Editing(), core.Revision() != rev, c.Cursor())
	}

	c.SetCursor(TablePos{0, 1})
	c.HandleKey(key('i'))
	typeInto(c, "!")
	c.HandleKey(altEnter)
	typeInto(c, "x")
	c.HandleKey(ctrlV)
	c.HandleKey(tabKey)
	typeInto(c, "y")
	c.HandleKey(enterKey)
	if got := src.Cell(0, 1); got != "one\ntwo!\nx\ty" {
		t.Fatalf("the value kept: %q", got)
	}
	undoes(t, core, text)
	core.Undo()
	src.Sync()
	c.Changed()

	c.SetCursor(TablePos{1, 0})
	c.HandleKey(key(tui.KeyF2))
	typeInto(c, "zz")
	c.HandleKey(escKey)
	if c.Editing() != nil || core.Value() != text {
		t.Fatalf("Esc wrote: %q", core.Value())
	}
	for _, app := range []string{" ", ":"} { // the application's: a leader, the command line
		if c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: []rune(app)[0], Text: app}) || c.Editing() != nil {
			t.Fatalf("%q opened the field", app)
		}
	}
	c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: 'q', Text: "q"}) // no command: opens with it typed
	if e := c.Editing(); e == nil || e.Field.Value() != "q" {
		t.Fatalf("a typed key: %+v", e)
	}
	c.HandleKey(tabKey) // keep, move right
	if src.Cell(1, 0) != "q" || c.Cursor() != (TablePos{1, 1}) {
		t.Fatalf("Tab kept %q, cursor %v", src.Cell(1, 0), c.Cursor())
	}
}

// TestTheEdgesInsertOnKeep: the edge row, the edge field and their corner are
// each one Commit and one undo step; Esc on an edge, or an edge kept empty, writes nothing; a
// source with no Commit, or a read-only one, offers no edges.
func TestTheEdgesInsertOnKeep(t *testing.T) {
	const text = "h,v\na,1\n"
	for _, cc := range []struct {
		name string
		at   TablePos
		want string
	}{
		{"bottom", TablePos{1, 0}, "h,v\na,1\nnew,\n"},
		{"right", TablePos{0, 2}, "h,v,\na,1,new\n"},
		{"corner", TablePos{1, 2}, "h,v,\na,1,\n,,new\n"},
	} {
		t.Run(cc.name, func(t *testing.T) {
			c, _, core, _ := csvTable(text, TableAppendEdges())
			if !c.EdgeRow() {
				t.Fatal("no edges")
			}
			c.SetCursor(cc.at)
			if c.Cursor() != cc.at {
				t.Fatalf("the cursor cannot reach the edge: %v", c.Cursor())
			}
			c.HandleKey(enterKey)
			c.HandleKey(escKey)
			c.HandleKey(enterKey)
			c.HandleKey(enterKey) // kept empty
			if core.Value() != text {
				t.Fatalf("Esc, or an empty keep, wrote: %q", core.Value())
			}
			c.SetCursor(cc.at)
			c.HandleKey(enterKey)
			typeInto(c, "new")
			c.HandleKey(enterKey)
			if core.Value() != cc.want {
				t.Fatalf("\n got %q\nwant %q", core.Value(), cc.want)
			}
			undoes(t, core, text)
		})
	}

	plain := NewEditorCore(CoreInitialText("| a | b |\n| --- | --- |\n| 1 | 2 |"))
	tt, _ := TableAt(plain, 0)
	c := NewTableCore(tt, TableAppendEdges())
	if c.EdgeRow() || c.OpenRow() {
		t.Fatal("a source with no Commit offers edges or o")
	}
	ro, _, core, _ := csvTable(text, TableAppendEdges())
	core.SetReadOnly(true)
	if ro.EdgeRow() || ro.EditCell("") {
		t.Fatal("a read-only core offers edges or a field")
	}
}

// TestOInsertsARowWhenKept: o shows a new row below the cursor, inserted only when its value is
// kept; Esc leaves the file as it was.
func TestOInsertsARowWhenKept(t *testing.T) {
	const text = "h,v\na,1\nb,2\n"
	c, src, core, _ := csvTable(text)
	c.HandleKey(key('o'))
	if e := c.Editing(); e == nil || !e.Inserting() || e.Pos != (TablePos{1, 0}) {
		t.Fatalf("o: %+v", e)
	}
	c.HandleKey(escKey)
	if core.Value() != text {
		t.Fatalf("o then Esc wrote %q", core.Value())
	}
	c.HandleKey(key('o'))
	typeInto(c, "mid")
	c.HandleKey(enterKey)
	if core.Value() != "h,v\na,1\nmid,\nb,2\n" || src.Len() != 3 {
		t.Fatalf("o kept: %q", core.Value())
	}
	undoes(t, core, text)
}

// TestDdAndXEdit: dd deletes the row (one step), x empties the cell; on a read-only source neither
// does anything, and Enter activates as on any table.
func TestDdAndXEdit(t *testing.T) {
	const text = "h,v\na,1\nb,2\n"
	c, src, core, _ := csvTable(text)
	c.HandleKey(key('d'))
	if core.Value() != text {
		t.Fatal("one d deleted")
	}
	c.HandleKey(key('d'))
	if core.Value() != "h,v\nb,2\n" {
		t.Fatalf("dd: %q", core.Value())
	}
	core.Undo()
	src.Sync()
	c.Changed()
	c.SetCursor(TablePos{1, 1})
	c.HandleKey(key('x'))
	if core.Value() != "h,v\na,1\nb,\n" {
		t.Fatalf("x: %q", core.Value())
	}

	activated := -1
	ro, _, rcore, _ := csvTable(text, TableOnActivate(func(r int) { activated = r }))
	rcore.SetReadOnly(true)
	ro.HandleKey(key('d'))
	ro.HandleKey(key('d'))
	ro.HandleKey(key('x'))
	ro.HandleKey(enterKey)
	if rcore.Value() != text || ro.Editing() != nil || activated != 0 {
		t.Fatalf("read only: %q, editing %v, activated %d", rcore.Value(), ro.Editing(), activated)
	}
}

// TestAnOpenEditIsFenced: a Raw edit, an undo, a sort, a filter and a new source
// each drop the open field, saying so, and nothing is written; a keep after the source's revision
// moved, unseen, is refused; a refusal by the source drops the edit. A control with no change keeps.
func TestAnOpenEditIsFenced(t *testing.T) {
	const text = "h,v\na,1\nb,2\n"
	open := func(c *TableCore) {
		c.SetCursor(TablePos{0, 1})
		c.EditCell("")
		typeInto(c, "9")
	}
	for _, cc := range []struct {
		name   string
		change func(*TableCore, *TableCSV, *EditorCore)
	}{
		{"a Raw edit", func(c *TableCore, s *TableCSV, e *EditorCore) { e.SetValue("h,v\nz,0\n"); s.Sync(); c.Changed() }},
		{"an undo", func(c *TableCore, s *TableCSV, e *EditorCore) {
			e.Undo() // the setup's own edit undone
			s.Sync()
			c.Changed()
		}},
		{"a sort", func(c *TableCore, _ *TableCSV, _ *EditorCore) { c.SortKeys([]TableSortKey{{Col: 0, Desc: true}}) }},
		{"a filter", func(c *TableCore, _ *TableCSV, _ *EditorCore) {
			p, err := ParseTableWhere(`h = 'a'`, c.Fields())
			if err != nil {
				panic(err)
			}
			c.SetFilter(p)
		}},
		{"a new source", func(c *TableCore, s *TableCSV, _ *EditorCore) {
			c.SetSource(NewTableCSV(NewEditorCore(CoreInitialText(text)), ','))
		}},
		{"the host", func(c *TableCore, _ *TableCSV, _ *EditorCore) { c.CancelEdit() }},
	} {
		t.Run(cc.name, func(t *testing.T) {
			c, src, core, dropped := csvTable(text)
			c.SetCursor(TablePos{1, 0})
			c.EditCell("")
			typeInto(c, "B") // the setup's own edit, one undo step
			c.HandleKey(enterKey)
			before := core.Value()
			open(c)
			cc.change(c, src, core)
			if c.Editing() != nil || *dropped != 1 {
				t.Fatalf("still editing %v, dropped %d", c.Editing(), *dropped)
			}
			c.HandleKey(enterKey) // the keep that would have written
			if v := core.Value(); v == "h,v\na,19\nbB,2\n" || (cc.name != "a Raw edit" && cc.name != "an undo" && v != before) {
				t.Fatalf("the dropped edit was written: %q", v)
			}
		})
	}

	c, _, core, dropped := csvTable(text)
	open(c)
	core.SetValue("h,v\nq,q\n") // unseen: no Changed
	if c.KeepEdit(1, 0) || *dropped != 1 || core.Value() != "h,v\nq,q\n" {
		t.Fatalf("a keep past a revision it did not see: %q, dropped %d", core.Value(), *dropped)
	}

	c, _, core, dropped = csvTable(text)
	open(c)
	core.SetReadOnly(true)
	if c.KeepEdit(1, 0) || *dropped != 1 || core.Value() != text {
		t.Fatalf("a refusal: %q, dropped %d", core.Value(), *dropped)
	}

	c, _, core, _ = csvTable(text)
	open(c)
	if !c.KeepEdit(1, 0) || core.Value() != "h,v\na,19\nb,2\n" {
		t.Fatalf("the control: %q", core.Value())
	}
}

// sliceRows is a slice-backed editable source with no revision: an uncomparable type, as a
// consumer's may be.
type sliceRows [][]string

func (s sliceRows) Fields() []TableField             { return []TableField{{Title: "a"}, {Title: "b"}} }
func (s sliceRows) Len() int                         { return len(s) }
func (s sliceRows) Cell(r, c int) string             { return s[r][c] }
func (s sliceRows) SetCell(r, c int, v string) bool  { s[r][c] = v; return true }
func (s sliceRows) InsertRow(int) bool               { return false }
func (s sliceRows) DeleteRow(int) bool               { return false }
func (s sliceRows) InsertField(int, TableField) bool { return false }
func (s sliceRows) DeleteField(int) bool             { return false }
func (s sliceRows) SetAlign(int, style.Align) bool   { return false }

// TestAnEditOnAnUncomparableSourceWithNoRevision: the fence never compares a source
// itself, so a slice-backed one edits without a panic; with no revision to tell a change apart, a
// Changed drops its open edit rather than let it overwrite whatever the cell now holds.
func TestAnEditOnAnUncomparableSourceWithNoRevision(t *testing.T) {
	rows := sliceRows{{"1", "2"}, {"3", "4"}}
	dropped := 0
	c := NewTableCore(rows, TableOnEditDropped(func() { dropped++ }))
	c.Bind(nil, &fakeTableLayout{})
	c.EditCell("x")
	if !c.KeepEdit(0, 0) || rows[0][0] != "x" {
		t.Fatalf("an edit on a slice source: %v", rows)
	}
	c.EditCell("y")
	rows[0][0] = "changed elsewhere"
	c.Changed()
	if c.Editing() != nil || dropped != 1 {
		t.Fatalf("a Changed under a revisionless edit: editing %v, dropped %d", c.Editing(), dropped)
	}
	c.HandleKey(enterKey) // opens a new field now; nothing of the old one is written
	c.HandleKey(escKey)
	if rows[0][0] != "changed elsewhere" {
		t.Fatalf("the dropped edit was written: %q", rows[0][0])
	}
}
