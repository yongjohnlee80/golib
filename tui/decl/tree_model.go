package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// TREE MODELS — Qt's QAbstractItemModel with children, loaded when asked.
//
// A row may have children the model has not loaded yet. The view asks —
// CanFetchMore, then FetchMore, on an expand — and the host loads them under
// its own generation and applies them with SetChildren, only if they still
// answer the question it is asking.
//
// A row can be opened when it has children (RowCount) or has them still to
// load (CanFetchMore) — the one answer, from the two facts that make it. Qt's
// hasChildren is that same answer by default, and a model overrides it only to
// say "still to load", which is what CanFetchMore already says; a third method
// could only disagree with the two.

// TreeModel extends [ItemModel] for hierarchical tree structures, supporting on-demand
// (lazy) asynchronous fetching of child nodes as branches expand.
//
// It mirrors Qt 6's QAbstractItemModel tree semantics:
//   - A node is considered expandable if it currently has children (RowCount > 0) OR if it
//     can load more children on demand (CanFetchMore is true).
//   - When a user expands a collapsed node in [TreeView], the view queries CanFetchMore; if true,
//     it calls FetchMore(ix) to request the child hierarchy.
//   - The host application loads the children asynchronously or synchronously and calls
//     [TreeListModel.SetChildren] to populate the branch and notify the view.
type TreeModel interface {
	ItemModel

	// CanFetchMore reports whether the item at ix has unloaded children pending retrieval.
	// Must remain true until children are delivered or confirmed non-existent.
	CanFetchMore(ix Index) bool

	// FetchMore initiates the retrieval of child items for the node at ix.
	// If a fetch operation is already in-flight for this node, subsequent calls should be ignored.
	FetchMore(ix Index)
}

// TreeRow defines a single tree node's data roles and its child availability.
type TreeRow struct {
	// Row contains the role-to-value mappings for this node (e.g. "label", "badge", "key").
	Row
	// HasChildren indicates whether this node has child rows to fetch or display.
	HasChildren bool
}

// TreeListModel is an in-memory, hierarchical tree data model that supports on-demand loading.
//
// # Usage and Lifecycle
//
// When a tree node with HasChildren=true is expanded, the view invokes [TreeModel.FetchMore],
// which triggers the OnFetch callback. The host populates the branch by calling [TreeListModel.SetChildren].
//
// # Thread Safety
//
// TreeListModel is single-threaded and loop-owned. Mutations and child updates must be executed
// on the UI event-loop goroutine.
type TreeListModel struct {
	roles   []string
	top     []*treeNode
	subs    map[int]func(Change)
	next    int
	// OnFetch is invoked when a view expands a node whose children have not yet been loaded.
	OnFetch func(Index)
}

type treeNode struct {
	row      Row
	has      bool
	loaded   bool
	fetching bool
	kids     []*treeNode
}

// NewTreeListModel constructs an empty [TreeListModel] with the specified role names.
func NewTreeListModel(roles ...string) *TreeListModel {
	return &TreeListModel{roles: append([]string(nil), roles...), subs: map[int]func(Change){}}
}

// SetChildren replaces the rows under parent (nil: the top level) and tells
// the views.
func (m *TreeListModel) SetChildren(parent *Index, rows []TreeRow) {
	kids := make([]*treeNode, len(rows))
	for i, r := range rows {
		kids[i] = &treeNode{row: r.Row, has: r.HasChildren}
	}
	if parent == nil {
		m.top = kids
	} else {
		n := m.node(*parent)
		if n == nil {
			return // a row that is gone: nothing to put them under
		}
		n.kids, n.loaded, n.fetching = kids, true, false
	}
	m.notify(Change{Kind: Reset, Parent: parent})
}

// node is the node at ix, nil when there is none.
func (m *TreeListModel) node(ix Index) *treeNode {
	rows := m.top
	if ix.Parent != nil {
		p := m.node(*ix.Parent)
		if p == nil {
			return nil
		}
		rows = p.kids
	}
	if ix.Row < 0 || ix.Row >= len(rows) {
		return nil
	}
	return rows[ix.Row]
}

// RowCount implements ItemModel.
func (m *TreeListModel) RowCount(parent *Index) int {
	if parent == nil {
		return len(m.top)
	}
	if n := m.node(*parent); n != nil {
		return len(n.kids)
	}
	return 0
}

// ColumnCount implements ItemModel: a tree is one column.
func (m *TreeListModel) ColumnCount(*Index) int { return 1 }

// Data implements ItemModel.
func (m *TreeListModel) Data(ix Index, role string) qml.SpecValue {
	n := m.node(ix)
	if n == nil {
		return qml.SpecValue{}
	}
	v, ok := n.row[role]
	if !ok {
		return qml.SpecValue{}
	}
	sv, err := Value(v)
	if err != nil {
		return qml.SpecValue{Kind: qml.SpecValueString, Raw: fmt.Sprint(v)}
	}
	return sv
}

// HeaderData implements ItemModel: a tree has no header.
func (m *TreeListModel) HeaderData(int) string { return "" }

// Roles implements ItemModel.
func (m *TreeListModel) Roles() []string { return append([]string(nil), m.roles...) }

// Key implements ItemModel: the row's `key` role, or its position.
func (m *TreeListModel) Key(ix Index) string {
	if n := m.node(ix); n != nil {
		if k, ok := n.row["key"]; ok {
			return fmt.Sprint(k)
		}
	}
	return fmt.Sprint(ix.Row)
}

// CanFetchMore implements TreeModel.
func (m *TreeListModel) CanFetchMore(ix Index) bool {
	n := m.node(ix)
	return n != nil && n.has && !n.loaded
}

// FetchMore implements TreeModel: it asks OnFetch, once.
func (m *TreeListModel) FetchMore(ix Index) {
	n := m.node(ix)
	if n == nil || !m.CanFetchMore(ix) || n.fetching {
		return // nothing to load, or its load is in flight
	}
	n.fetching = true
	if m.OnFetch != nil {
		m.OnFetch(ix)
	}
}

// Subscribe implements ItemModel.
func (m *TreeListModel) Subscribe(fn func(Change)) (cancel func()) {
	id := m.next
	m.next++
	m.subs[id] = fn
	return func() { delete(m.subs, id) }
}

// Subscribers is how many views follow the model.
func (m *TreeListModel) Subscribers() int { return len(m.subs) }

func (m *TreeListModel) notify(c Change) {
	for _, fn := range m.subs {
		fn(c)
	}
}

var _ TreeModel = (*TreeListModel)(nil)
