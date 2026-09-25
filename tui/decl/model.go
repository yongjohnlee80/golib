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

// ItemModel defines the contract for data models displayed by declarative terminal views
// (ListView, ComboBox, TableView). It corresponds conceptually to Qt's QAbstractItemModel,
// streamlined for terminal UI layout: tabular or list rows, typed data roles, column headers,
// and change subscriptions.
//
// # Architectural Invariants
//
//   - Thread Affinity: All ItemModel methods and subscription notifications execute on the
//     application UI loop goroutine. Background worker goroutines must marshal modifications
//     onto the loop via [Program.Post].
//   - Key Persistence: Key(ix) must return a persistent, unique identifier for each row.
//     Views track user selection by row key rather than ordinal index, ensuring that active
//     selections remain attached to the same record across additions, deletions, and sorts.
type ItemModel interface {
	// RowCount returns the number of rows located under parent (nil for top-level rows).
	RowCount(parent *Index) int

	// ColumnCount returns the number of columns present under parent (1 for linear lists).
	ColumnCount(parent *Index) int

	// Data returns the typed value of role for the cell addressed by ix.
	// If role is empty (""), tabular views expect the role assigned to column ix.Column.
	// Returns a zero [qml.SpecValue] if the cell or role does not exist.
	Data(ix Index, role string) qml.SpecValue

	// HeaderData returns the display title for the specified column index.
	HeaderData(column int) string

	// Roles lists all valid role names provided by this model (similar to Qt's roleNames).
	Roles() []string

	// Key returns a stable, unique identifier for the row at ix.
	Key(ix Index) string

	// Subscribe registers fn to receive model change notifications on the application loop.
	// The returned cancel function stops further deliveries and unregisters the subscriber.
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

// Row represents a single record within a [ListModel], mapping role names to typed values
// (strings, booleans, integers, or floats).
type Row map[string]any

// ListModel is an in-memory, flat tabular data model for declarative views (ListView, ComboBox, TableView).
// It models Qt Quick's ListModel.
//
// # Key Selection and Row Identity
//
// If a row contains a "key" role, its value is used as the row's persistent identity.
// If the "key" role is omitted, the row falls back to its positional string index ("0", "1", ...).
// When configured with columns via [ListModel.SetColumns], the model functions as a multi-column table.
//
// # Concurrency
//
// ListModel is loop-owned and must be mutated only on the UI goroutine.
type ListModel struct {
	roles   []string
	columns []Column
	rows    []Row
	subs    map[int]func(Change)
	next    int
}

// Column defines a single column in a tabular [ListModel], pairing a model role with a header title.
type Column struct {
	// Role is the role name whose value is presented in this column.
	Role string
	// Title is the column header text displayed by TableView.
	Title string
}

// NewListModel constructs an empty [ListModel] registered with the provided role names.
func NewListModel(roles ...string) *ListModel {
	return &ListModel{roles: append([]string(nil), roles...), subs: map[int]func(Change){}}
}

// SetColumns configures the model as a table with the specified columns and emits ColumnsReset to all views.
func (m *ListModel) SetColumns(cols ...Column) {
	m.columns = append([]Column(nil), cols...)
	m.notify(Change{Kind: ColumnsReset})
}

// Reset replaces all rows in the model with rows and emits a Reset change notification.
func (m *ListModel) Reset(rows []Row) {
	m.rows = append([]Row(nil), rows...)
	m.notify(Change{Kind: Reset})
}

// Set updates row i with the contents of r and emits a Changed notification for that row.
func (m *ListModel) Set(i int, r Row) {
	m.rows[i] = r
	m.notify(Change{Kind: Changed, First: i, Last: i})
}

// Insert inserts rows immediately before row index i (if i == Len(), rows are appended).
// Emits an Inserted change notification covering the new row range.
func (m *ListModel) Insert(i int, rows ...Row) {
	if len(rows) == 0 {
		return
	}
	out := append(append(append([]Row(nil), m.rows[:i]...), rows...), m.rows[i:]...)
	m.rows = out
	m.notify(Change{Kind: Inserted, First: i, Last: i + len(rows) - 1})
}

// Remove deletes n rows starting at row index i and emits a Removed change notification.
func (m *ListModel) Remove(i, n int) {
	if n <= 0 {
		return
	}
	m.rows = append(m.rows[:i:i], m.rows[i+n:]...)
	m.notify(Change{Kind: Removed, First: i, Last: i + n - 1})
}

// Len returns the current number of rows in the model.
func (m *ListModel) Len() int { return len(m.rows) }

// At returns the row record located at index i.
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
