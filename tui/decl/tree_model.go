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

// TreeModel is an ItemModel whose rows have children.
type TreeModel interface {
	ItemModel
	// CanFetchMore reports whether a row has children still to load: true
	// until they have arrived — including while a load is in flight, as Qt's
	// canFetchMore is.
	CanFetchMore(ix Index) bool
	// FetchMore asks for a row's children. A request while one is in flight
	// is the model's to ignore.
	FetchMore(ix Index)
}

// TreeRow is a row of a TreeListModel, and whether it has children to load.
type TreeRow struct {
	Row
	HasChildren bool
}

// TreeListModel is golib's tree model: rows of named roles in memory, each
// with children the host sets when asked. OnFetch is how it asks — the view
// expanded a row whose children are not loaded — and SetChildren is the
// answer. Loop-owned.
type TreeListModel struct {
	roles   []string
	top     []*treeNode
	subs    map[int]func(Change)
	next    int
	OnFetch func(Index)
}

type treeNode struct {
	row      Row
	has      bool
	loaded   bool
	fetching bool
	kids     []*treeNode
}

// NewTreeListModel is an empty tree with the given roles.
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
