package decl

import "github.com/yongjohnlee80/golib/parse/qml"

// MODELS — the engine's view of a host's data model, for a Repeater.
//
// A Repeater instantiates its delegate once per row of a model; that is all
// the engine asks of one. A toolkit's model contract (tui/decl's ItemModel)
// is a superset, with columns and headers for its views.

// Index addresses one cell: its row and column under Parent (nil at the top).
type Index struct {
	Row, Column int
	Parent      *Index
}

// ChangeKind is what a Change says happened.
type ChangeKind uint8

const (
	// Reset: everything may have changed.
	Reset ChangeKind = iota
	// Changed: rows First..Last changed in place.
	Changed
	// Inserted: rows First..Last are new.
	Inserted
	// Removed: rows First..Last are gone.
	Removed
	// ColumnsReset: the columns and their titles changed, and every row.
	ColumnsReset
)

// Change is one change to a model, under Parent (nil: the top level).
type Change struct {
	Kind        ChangeKind
	Parent      *Index
	First, Last int
}

// Model is what a Repeater reads of a host object: its rows, each row's typed
// roles and stable key, and its changes.
type Model interface {
	RowCount(parent *Index) int
	Data(ix Index, role string) qml.SpecValue
	Key(ix Index) string
	Subscribe(fn func(Change)) (cancel func())
}
