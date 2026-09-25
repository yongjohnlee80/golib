package decl

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/decl"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// MODELS — Qt's model/view: the host supplies the data, QML declares the view.
//
//	ListView { model: App.connections; textRole: "label" }
//
// A model is a Go object behind a source (`App.connections`): the host builds
// it, sets it once, and changes it; the views bound to it follow its changes
// without the document rebinding anything. It is Qt's QAbstractItemModel, cut
// to what a terminal view shows: rows, their columns, and named ROLES whose
// values are typed — a string, a bool, a number.
//
// A model is loop-owned, like everything a document reads: a host changing it
// from a worker posts the change (Program.Post).

// Index, Change and ChangeKind are the engine's (decl): a Repeater reads the
// same models the views show.
type (
	Index      = decl.Index
	Change     = decl.Change
	ChangeKind = decl.ChangeKind
)

// ItemModel is a model a view can show — Qt's QAbstractItemModel.
type ItemModel interface {
	// RowCount is the number of rows under parent (nil: the top level).
	RowCount(parent *Index) int
	// ColumnCount is the number of columns: 1 for a list.
	ColumnCount(parent *Index) int
	// Data is one role of one cell, typed; the zero value for a role the
	// model does not have.
	Data(ix Index, role string) qml.SpecValue
	// HeaderData is a table column's title.
	HeaderData(column int) string
	// Roles are the role names the model answers — Qt's roleNames.
	Roles() []string
	// Key is a row's stable identity: the same row keeps its key as rows are
	// inserted and removed around it — Qt's persistent index.
	Key(ix Index) string
	// Subscribe delivers every change, on the loop, until cancel.
	Subscribe(fn func(Change)) (cancel func())
}

// The change kinds, as the engine names them.
const (
	Reset        = decl.Reset
	Changed      = decl.Changed
	Inserted     = decl.Inserted
	Removed      = decl.Removed
	ColumnsReset = decl.ColumnsReset
)

// Row is one row of a ListModel: a value per role. Values are strings, bools
// and numbers.
type Row map[string]any

// ListModel is golib's model — Qt's QML ListModel: rows of named roles, held
// in memory. `key` is the role a row's identity is read from; a row without it
// is keyed by position. Columns, when set, make it a table: each column shows
// one role, under its title.
//
// Loop-owned: change it on the UI loop, where views read it.
type ListModel struct {
	roles   []string
	columns []Column
	rows    []Row
	subs    map[int]func(Change)
	next    int
}

// Column is one table column of a ListModel: the role it shows, and its title.
type Column struct {
	Role, Title string
}

// NewListModel is an empty model with the given roles.
func NewListModel(roles ...string) *ListModel {
	return &ListModel{roles: append([]string(nil), roles...), subs: map[int]func(Change){}}
}

// SetColumns makes the model a table of these columns, and tells the views.
func (m *ListModel) SetColumns(cols ...Column) {
	m.columns = append([]Column(nil), cols...)
	m.notify(Change{Kind: ColumnsReset})
}

// Reset replaces every row.
func (m *ListModel) Reset(rows []Row) {
	m.rows = append([]Row(nil), rows...)
	m.notify(Change{Kind: Reset})
}

// Set replaces row i.
func (m *ListModel) Set(i int, r Row) {
	m.rows[i] = r
	m.notify(Change{Kind: Changed, First: i, Last: i})
}

// Insert puts rows before row i (i == Len appends).
func (m *ListModel) Insert(i int, rows ...Row) {
	if len(rows) == 0 {
		return
	}
	out := append(append(append([]Row(nil), m.rows[:i]...), rows...), m.rows[i:]...)
	m.rows = out
	m.notify(Change{Kind: Inserted, First: i, Last: i + len(rows) - 1})
}

// Remove drops n rows from row i.
func (m *ListModel) Remove(i, n int) {
	if n <= 0 {
		return
	}
	m.rows = append(m.rows[:i:i], m.rows[i+n:]...)
	m.notify(Change{Kind: Removed, First: i, Last: i + n - 1})
}

// Len is the number of rows.
func (m *ListModel) Len() int { return len(m.rows) }

// At is row i.
func (m *ListModel) At(i int) Row { return m.rows[i] }

// RowCount implements ItemModel: a ListModel is flat.
func (m *ListModel) RowCount(parent *Index) int {
	if parent != nil {
		return 0
	}
	return len(m.rows)
}

// ColumnCount implements ItemModel.
func (m *ListModel) ColumnCount(parent *Index) int {
	if parent != nil {
		return 0
	}
	return max(len(m.columns), 1)
}

// Data implements ItemModel. Column c of a table shows its column's role; a
// named role reads the row's value.
func (m *ListModel) Data(ix Index, role string) qml.SpecValue {
	if ix.Parent != nil || ix.Row < 0 || ix.Row >= len(m.rows) {
		return qml.SpecValue{}
	}
	if role == "" && ix.Column < len(m.columns) {
		role = m.columns[ix.Column].Role
	}
	v, ok := m.rows[ix.Row][role]
	if !ok {
		return qml.SpecValue{}
	}
	sv, err := Value(v)
	if err != nil {
		return qml.SpecValue{Kind: qml.SpecValueString, Raw: fmt.Sprint(v)}
	}
	return sv
}

// HeaderData implements ItemModel.
func (m *ListModel) HeaderData(column int) string {
	if column < 0 || column >= len(m.columns) {
		return ""
	}
	return m.columns[column].Title
}

// Roles implements ItemModel.
func (m *ListModel) Roles() []string { return append([]string(nil), m.roles...) }

// Key implements ItemModel: the row's `key` role, or its position.
func (m *ListModel) Key(ix Index) string {
	if ix.Row >= 0 && ix.Row < len(m.rows) {
		if k, ok := m.rows[ix.Row]["key"]; ok {
			return fmt.Sprint(k)
		}
	}
	return strconv.Itoa(ix.Row)
}

// Subscribe implements ItemModel.
func (m *ListModel) Subscribe(fn func(Change)) (cancel func()) {
	id := m.next
	m.next++
	m.subs[id] = fn
	return func() { delete(m.subs, id) }
}

func (m *ListModel) notify(c Change) {
	for _, fn := range m.subs {
		fn(c)
	}
}

// Subscribers is how many views follow the model — for a test to prove none
// leaked.
func (m *ListModel) Subscribers() int { return len(m.subs) }

var (
	_ ItemModel  = (*ListModel)(nil)
	_ decl.Model = (*ListModel)(nil)
)

// modelOf reads a model a document bound: `model: App.connections`.
func modelOf(v qml.SpecValue) (ItemModel, error) {
	if v.Kind != qml.SpecValueObject {
		return nil, fmt.Errorf("want a model, got %s (at %s)", v.Kind, v.Pos)
	}
	m, ok := v.Obj.(ItemModel)
	if !ok {
		return nil, fmt.Errorf("want a model, got a %T (at %s)", v.Obj, v.Pos)
	}
	return m, nil
}

// indexValue is how a view's signal carries the row it was raised for: an
// Index, as an object — only a signal makes one; a host cannot bind it as a
// source (Value refuses it).
func indexValue(ix Index) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueObject, Obj: ix}
}
