package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// model_test.go holds ListModel to Qt's model contract: typed roles, keys that
// survive inserts, columns and titles, and a change for every mutation.

func TestAListModelReportsEveryChangeAndKeepsItsKeys(t *testing.T) {
	m := tuidecl.NewListModel("key", "name", "on", "n")
	var changes []tuidecl.Change
	cancel := m.Subscribe(func(c tuidecl.Change) { changes = append(changes, c) })
	m.Reset([]tuidecl.Row{{"key": "a", "name": "ann", "on": true, "n": 7}, {"name": "unkeyed"}})
	m.Insert(0, tuidecl.Row{"key": "z", "name": "zed"})
	m.Set(1, tuidecl.Row{"key": "a", "name": "ANN", "on": false})
	m.Remove(0, 1)
	m.SetColumns(tuidecl.Column{Role: "name", Title: "NAME"}, tuidecl.Column{Role: "n", Title: "N"})
	want := []tuidecl.ChangeKind{tuidecl.Reset, tuidecl.Inserted, tuidecl.Changed, tuidecl.Removed, tuidecl.ColumnsReset}
	if len(changes) != len(want) {
		t.Fatalf("changes %+v, want kinds %v", changes, want)
	}
	for i, k := range want {
		if changes[i].Kind != k {
			t.Errorf("change %d is %v, want %v", i, changes[i].Kind, k)
		}
	}
	if c := changes[1]; c.First != 0 || c.Last != 0 {
		t.Errorf("insert reported rows %d..%d, want 0..0", c.First, c.Last)
	}
	if m.Len() != 2 || m.Key(tuidecl.Index{Row: 0}) != "a" || m.Key(tuidecl.Index{Row: 1}) != "1" {
		t.Errorf("keys %q %q, want a and the unkeyed row's position 1",
			m.Key(tuidecl.Index{Row: 0}), m.Key(tuidecl.Index{Row: 1}))
	}
	on := m.Data(tuidecl.Index{Row: 0}, "on")
	if on.Kind != qml.SpecValueBool || on.Raw != "false" {
		t.Errorf("a bool role reads %+v, want a typed false", on)
	}
	if v := m.Data(tuidecl.Index{Row: 0, Column: 0}, ""); v.Raw != "ANN" {
		t.Errorf("column 0 reads %q, want its role's value ANN", v.Raw)
	}
	if m.HeaderData(1) != "N" || m.HeaderData(5) != "" || m.ColumnCount(nil) != 2 {
		t.Errorf("headers %q %q, columns %d", m.HeaderData(1), m.HeaderData(5), m.ColumnCount(nil))
	}
	if v := m.Data(tuidecl.Index{Row: 9}, "name"); v.Kind != qml.SpecValueInvalid {
		t.Errorf("a row out of range reads %+v, want nothing", v)
	}
	if m.RowCount(&tuidecl.Index{}) != 0 || len(m.Roles()) != 4 || m.At(1)["name"] != "unkeyed" {
		t.Error("a flat model has rows under a parent, lost a role, or moved a row")
	}
	cancel()
	m.Reset(nil)
	if len(changes) != len(want) || m.Subscribers() != 0 {
		t.Errorf("a cancelled subscription still hears changes (%d) or is counted (%d)", len(changes), m.Subscribers())
	}
}
