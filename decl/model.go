package decl

import "github.com/yongjohnlee80/golib/parse/qml"

// # Data Models for Declarative UI
//
// Model defines the declarative engine's view of a reactive host data source,
// consumed primarily by dynamic container elements such as Repeater and Instantiator.
//
// A Repeater instantiates its delegate once per row of a Model; that is all the
// core engine requires. UI toolkit model contracts (such as [tui/decl.ItemModel])
// form a strict superset of this interface, providing additional capabilities like
// tabular columns, header metadata, and visual formatting for views (ListView,
// TableView, TreeView).

// Index addresses a single item or cell within a hierarchical or flat data model.
//
// # Coordinate Invariants
//
//   - Row: 0-indexed position within the siblings under Parent. Must satisfy 0 <= Row < RowCount(Parent).
//   - Column: 0-indexed column position. For flat lists or tree nodes without columns, Column is 0.
//   - Parent: Pointer to the parent item's Index. For top-level root items, Parent is nil.
type Index struct {
	Row, Column int
	Parent      *Index
}

// ChangeKind specifies the operational mutation represented by a [Change] notification.
type ChangeKind uint8

const (
	// Reset indicates that the model's structure or content has been completely replaced.
	// Consumers must invalidate all cached indices and re-query the full row hierarchy.
	Reset ChangeKind = iota

	// Changed indicates that existing rows from First to Last (inclusive) were modified in place.
	// Their identities and keys remain intact, but role data values have updated.
	Changed

	// Inserted indicates that new rows from First to Last (inclusive) have been added to the model.
	Inserted

	// Removed indicates that rows from First to Last (inclusive) were deleted from the model.
	Removed

	// ColumnsReset indicates that tabular column schemas, headers, or roles have changed,
	// necessitating a full view layout and header refresh across every row.
	ColumnsReset
)

// String returns a human-readable representation of the change kind.
func (k ChangeKind) String() string {
	switch k {
	case Reset:
		return "Reset"
	case Changed:
		return "Changed"
	case Inserted:
		return "Inserted"
	case Removed:
		return "Removed"
	case ColumnsReset:
		return "ColumnsReset"
	default:
		return "UnknownChange"
	}
}

// Change describes an atomic structural or data mutation emitted by a [Model].
//
// Mutations are scoped under Parent (nil for top-level root items). First and Last
// represent inclusive sibling row indices [First, Last] within that parent scope.
type Change struct {
	// Kind describes the nature of the mutation (Reset, Changed, Inserted, Removed, ColumnsReset).
	Kind ChangeKind

	// Parent addresses the parent node whose children were affected (nil for top-level rows).
	Parent *Index

	// First is the starting row index (0-indexed, inclusive) of the affected range.
	First int

	// Last is the ending row index (0-indexed, inclusive) of the affected range.
	Last int
}

// Model represents the minimal reactive data source contract required by the
// declarative engine for dynamic expansion (Repeater, Instantiator).
//
// # Thread Ownership and Invariants
//
// All Model methods (RowCount, Data, Key, and Subscribe deliveries) execute on the
// application loop goroutine. If background workers or goroutines produce data updates,
// mutations must be marshalled through the application scheduler (e.g. Program.Post).
//
// # Key Stability
//
// Key must return a non-empty, unique, and persistent identifier for the row at Index.
// As rows are inserted, deleted, or reordered around it, a row's Key MUST remain stable.
// The engine's reconciler relies on Key to preserve widget state, focus, and identity
// across model mutations without triggering unnecessary teardown and reconstruction.
type Model interface {
	// RowCount returns the number of child rows located immediately under parent (nil for top-level).
	RowCount(parent *Index) int

	// Data retrieves the typed value of role for the row at ix.
	// If the role is unrecognised or unset, it returns a zero [qml.SpecValue].
	Data(ix Index, role string) qml.SpecValue

	// Key returns a stable, unique string identity for the row at ix.
	// Used by Repeaters and Instantiators to match nodes during reconciliation.
	Key(ix Index) string

	// Subscribe registers fn to receive change notifications emitted by the model.
	// It returns a cancel function that unsubscribes the caller.
	// The model MUST execute fn synchronously on the UI event-loop goroutine.
	Subscribe(fn func(Change)) (cancel func())
}
