package decl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TreeView presents a hierarchical, collapsible tree structure driven by a [TreeModel],
// mirroring Qt Quick Controls' TreeView.
//
//	TreeView {
//	    model: App.explorer              // a TreeModel reactive source
//	    textRole: "label"                // node display text
//	    badgeRole: "badge"               // optional numeric or string badge
//	    onActivated: App.open(index)     // fired on Enter or activation
//	    onExpanded: App.opened(index)    // fired when a branch expands
//	}
//
// # Lazy Fetching and Asynchronous Expansion
//
// TreeView integrates with the [TreeModel] lazy loading lifecycle:
//   - When a node is expanded for the first time and the model indicates unloaded children
//     ([TreeModel.CanFetchMore] is true), the view calls [TreeModel.FetchMore].
//   - Once the host populates children via [TreeListModel.SetChildren], the view reconstructs
//     the sub-hierarchy dynamically.
//   - An in-flight request tracker (`pending`) prevents duplicate concurrent fetches for the
//     same branch.
//
// # Signal Signatures and Parameter Types
//
// Handlers receive the targeted node's address as an [Index] object passed to the `index` parameter:
//   - `activated(index)`: Emitted when the user activates an item (Enter key, double-click).
//   - `expanded(index)`: Emitted when a branch transitions from collapsed to expanded.
//   - The Index is passed directly back to host handlers and model methods to uniquely address
//     the target node in the model hierarchy.
type treeViewNode struct {
	widget.Base
	model     TreeModel
	cancel    func()
	textRole  string
	badgeRole string
	tree      *widget.Tree
	// at is each shown node's Index; byPath each node by its path of keys;
	// pending the expand requests the model is still answering, by path.
	at        map[*widget.TreeNode]Index
	byPath    map[string]*widget.TreeNode
	pending   map[string]uint64
	activated func(args ...qml.SpecValue)
	expanded  func(args ...qml.SpecValue)
}

func buildTreeView(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a TreeView takes no children; its rows are its model's (at %s)", b.Pos)
	}
	n := &treeViewNode{activated: b.EmitterWith("activated"), expanded: b.EmitterWith("expanded")}
	consumed, err := readProps(b.Props, map[string]field{
		"textRole":  into(&n.textRole, stringOf),
		"badgeRole": into(&n.badgeRole, stringOf),
	})
	if err != nil {
		return nil, nil, err
	}
	n.tree = widget.NewTree()
	return n, consumed, nil
}

func treeModelOf(v qml.SpecValue) (TreeModel, error) {
	m, err := modelOf(v)
	if err != nil {
		return nil, err
	}
	tm, ok := m.(TreeModel)
	if !ok {
		return nil, fmt.Errorf("a TreeView wants a tree model; a %T has no children (at %s)", m, v.Pos)
	}
	return tm, nil
}

func (n *treeViewNode) setModel(m TreeModel) {
	n.release()
	n.model = m
	if m != nil {
		n.cancel = m.Subscribe(n.follow)
	}
	n.rebuild()
}

func (n *treeViewNode) release() {
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
}

// pathOf is an Index's path of keys: its identity in the tree. Each key is
// written with its length first — `3:a/b/1:c` — so a key holding the separator
// cannot make two paths one.
func (n *treeViewNode) pathOf(ix Index) string {
	var keys []string
	for p := &ix; p != nil; p = p.Parent {
		k := n.model.Key(*p)
		keys = append([]string{strconv.Itoa(len(k)) + ":" + k}, keys...)
	}
	return strings.Join(keys, "/")
}

// forget drops what the view knew of every row under path — its children are
// being replaced, so they are no longer shown.
func (n *treeViewNode) forget(path string) {
	prefix := path + "/"
	for p, node := range n.byPath {
		if strings.HasPrefix(p, prefix) {
			delete(n.byPath, p)
			delete(n.at, node)
			delete(n.pending, p)
		}
	}
}

// nodes are the tree nodes for the rows under parent.
func (n *treeViewNode) nodes(parent *Index) []*widget.TreeNode {
	count := n.model.RowCount(parent)
	out := make([]*widget.TreeNode, count)
	for r := 0; r < count; r++ {
		ix := Index{Row: r, Parent: parent}
		var opts []widget.NodeOption
		if n.model.RowCount(&ix) == 0 && !n.model.CanFetchMore(ix) {
			opts = append(opts, widget.WithLeaf()) // nothing to open, now or to load
		}
		if n.badgeRole != "" {
			if b := n.model.Data(ix, n.badgeRole).Raw; b != "" {
				opts = append(opts, widget.WithBadge(b))
			}
		}
		node := widget.NewTreeNode(n.model.Key(ix), n.model.Data(ix, n.textRole).Raw, opts...)
		n.at[node] = ix
		n.byPath[n.pathOf(ix)] = node
		out[r] = node
	}
	return out
}

// rebuild shows the model's top level afresh.
func (n *treeViewNode) rebuild() {
	n.at, n.byPath, n.pending = map[*widget.TreeNode]Index{}, map[string]*widget.TreeNode{}, map[string]uint64{}
	if n.model == nil {
		n.tree.SetRoots()
		return
	}
	n.tree.SetRoots(n.nodes(nil)...)
}

// follow applies a model change: a top-level reset rebuilds; children set
// under a row answer that row's expand request.
func (n *treeViewNode) follow(c Change) {
	if c.Parent == nil {
		n.rebuild()
		return
	}
	path := n.pathOf(*c.Parent)
	node, ok := n.byPath[path]
	if !ok {
		return // a row this view is not showing
	}
	n.forget(path) // the rows being replaced
	if gen, waiting := n.pending[path]; waiting {
		delete(n.pending, path)
		node.SetChildren(gen, n.nodes(c.Parent))
		return
	}
	// Children changed under a row already open: it closes and reloads on
	// the next open.
	node.Reset()
}

func (n *treeViewNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.tree)
	tui.SubscribeScoped(ctx, func(ev widget.ExpandRequestEvent) {
		if ev.Owner != n.tree.NodeID() || n.model == nil {
			return
		}
		ix, ok := n.at[ev.Node]
		if !ok {
			return
		}
		if n.model.RowCount(&ix) > 0 && !n.model.CanFetchMore(ix) {
			ev.Node.SetChildren(ev.Gen, n.nodes(&ix))
		} else {
			n.pending[n.pathOf(ix)] = ev.Gen
			if n.model.CanFetchMore(ix) {
				n.model.FetchMore(ix) // the model ignores a repeat while one is in flight
			}
		}
	})
	tui.SubscribeScoped(ctx, func(ev widget.ExpandEvent) {
		if ev.Owner != n.tree.NodeID() {
			return
		}
		if ix, ok := n.at[ev.Node]; ok {
			n.expanded(indexValue(ix))
		}
	})
	tui.SubscribeScoped(ctx, func(ev widget.ActivateEvent) {
		if ev.Owner != n.tree.NodeID() {
			return
		}
		rows := n.tree.VisibleRows()
		if ev.Index < 0 || ev.Index >= len(rows) {
			return
		}
		if ix, ok := n.at[rows[ev.Index]]; ok {
			n.activated(indexValue(ix))
		}
	})
}

func (n *treeViewNode) Layout(c tui.Constraints) tui.Size {
	sz := n.Context().LayoutChild(n.tree, c)
	n.Context().PlaceChild(n.tree, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (*treeViewNode) Render(tui.Surface)         {}
func (*treeViewNode) HandleEvent(tui.Event) bool { return false }

var treeViewType = Type{
	Name:  "TreeView",
	Build: buildTreeView,
	Ctor:  []string{"textRole", "badgeRole"},
	Setters: map[string]Setter{
		"model": setter("a TreeView", treeModelOf, func(n *treeViewNode, m TreeModel) { n.setModel(m) }),
	},
	Signals:   map[string][]string{"activated": {"index"}, "expanded": {"index"}},
	Destroyed: func(c tui.Component) { c.(*treeViewNode).release() },
}
