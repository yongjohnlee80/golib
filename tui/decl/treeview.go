package decl

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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
//	    onCurrentIndexChanged: App.moved(index) // the row under the cursor
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
//   - `currentIndexChanged(index)`: Emitted when a different row comes under the
//     cursor, by the keyboard, the mouse or setCurrentIndex, as ListView's.
//
// # Methods
//
//   - `toggleExpanded(index)`: opens or closes a shown row, Qt's toggleExpanded.
//   - `setCurrentIndex(index)`: puts the cursor on a row, opening its closed
//     ancestors first, as QAbstractItemView.setCurrentIndex with Qt's
//     expandToIndex. The ancestors' children must already be loaded in the
//     model; the view does not fetch to reach a row. See setCurrentIndex.
//   - The Index is passed directly back to host handlers and model methods to uniquely address
//     the target node in the model hierarchy.
type treeViewNode struct {
	widget.Base
	model     TreeModel
	cancel    func()
	textRole  string
	badgeRole string
	tree      *widget.Tree
	template  *qml.SpecNode
	eval      func(qml.SpecValue, map[string]qml.SpecValue) (qml.SpecValue, error)
	normal    style.Color
	sink      func(error)
	// at is each shown node's Index; byPath each node by its path of keys;
	// pending the expand requests the model is still answering, by path.
	at        map[*widget.TreeNode]Index
	byPath    map[string]*widget.TreeNode
	pending   map[string]uint64
	activated func(args ...qml.SpecValue)
	expanded  func(args ...qml.SpecValue)
	moved     func(args ...qml.SpecValue)
	// reveal is a setCurrentIndex still opening its row's ancestors: the
	// row's keys from the top, and how many more settled expansions it waits
	// for before giving up. Nil when none is in flight.
	reveal *treeReveal
	// lastMoved is the path of the row currentIndexChanged last named, so a
	// queued selection event that names the same row is not sent twice.
	lastMoved string
}

type treeReveal struct {
	keys   []string
	parts  []string // each key in pathOf's length-prefixed form
	rounds int
}

// path is the reveal's first depth+1 levels in pathOf's form.
func (r *treeReveal) path(depth int) string { return strings.Join(r.parts[:depth+1], "/") }

func buildTreeView(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a TreeView takes no children; its rows are its model's (at %s)", b.Pos)
	}
	n := &treeViewNode{activated: b.EmitterWith("activated"), expanded: b.EmitterWith("expanded"),
		moved: b.EmitterWith("currentIndexChanged"), eval: b.Eval, normal: style.Default(), sink: b.sink}
	consumed, err := readProps(b.Props, map[string]field{
		"textRole":  into(&n.textRole, stringOf),
		"badgeRole": into(&n.badgeRole, stringOf),
	})
	if err != nil {
		return nil, nil, err
	}
	for _, prop := range b.Props {
		if prop.Name != "delegate" {
			continue
		}
		if prop.Value.Kind != qml.SpecValueTemplate || prop.Value.Template == nil {
			return nil, nil, fmt.Errorf("TreeView.delegate takes a Text object template (at %s)", prop.Pos)
		}
		n.template = prop.Value.Template
		if n.template.Type != "Text" || n.template.ID != "" || len(n.template.Children) != 0 || len(n.template.Handlers) != 0 {
			return nil, nil, fmt.Errorf("TreeView.delegate supports stateless Text only, without ids, handlers or children (at %s)", n.template.Pos)
		}
		seen := map[string]bool{}
		for _, p := range n.template.Props {
			if (p.Name != "text" && p.Name != "color") || seen[p.Name] {
				return nil, nil, fmt.Errorf("TreeView.delegate Text accepts one text and optional color, not %q (at %s)", p.Name, p.Pos)
			}
			seen[p.Name] = true
		}
		if !seen["text"] {
			return nil, nil, fmt.Errorf("TreeView.delegate Text needs text (at %s)", n.template.Pos)
		}
		consumed = append(consumed, "delegate")
	}
	n.tree = widget.NewTree()
	if err := n.checkTemplate(nil); err != nil {
		return nil, nil, err
	}
	return n, consumed, nil
}

// checkTemplate validates row-role references even when the current model has
// no rows. Presentation is stateless: the model supplies facts and the QML
// template supplies the look.
func (n *treeViewNode) checkTemplate(m TreeModel) error {
	if n.template == nil {
		return nil
	}
	locals := map[string]qml.SpecValue{
		"palette.text": {Kind: qml.SpecValueObject, Obj: n.normal},
	}
	known := map[string]bool{}
	if m != nil {
		for _, role := range m.Roles() {
			known[role] = true
		}
	}
	for _, p := range n.template.Props {
		register := func(role string) error {
			if m != nil && !known[role] {
				return fmt.Errorf("TreeView.delegate: unknown model role %q (at %s)", role, p.Pos)
			}
			locals["model."+role] = qml.SpecValue{Kind: qml.SpecValueString, Raw: "normal"}
			return nil
		}
		if p.Value.Kind == qml.SpecValueRef && len(p.Value.Path) == 2 && p.Value.Path[0] == "model" {
			if err := register(p.Value.Path[1]); err != nil {
				return err
			}
		}
		if p.Value.Expr != nil {
			var refErr error
			p.Value.Expr.Walk(func(e *js.Expr) bool {
				if e.Kind == js.ExprMember && e.Left != nil && e.Left.Kind == js.ExprIdent && e.Left.Raw == "model" {
					if err := register(e.Name); err != nil {
						refErr = err
						return false
					}
				}
				return true
			})
			if refErr != nil {
				return refErr
			}
		}
		v, err := n.eval(p.Value, locals)
		if err != nil {
			return fmt.Errorf("TreeView.delegate %s (at %s): %w", p.Name, p.Pos, err)
		}
		if p.Name == "color" {
			if _, err := colorOf(v); err != nil {
				return err
			}
		}
	}
	return nil
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

func (n *treeViewNode) present(ix Index) (string, style.Style) {
	label := n.model.Data(ix, n.textRole).Raw
	if n.template == nil {
		return label, style.Style{}
	}
	locals := map[string]qml.SpecValue{
		"palette.text": {Kind: qml.SpecValueObject, Obj: n.normal},
	}
	for _, role := range n.model.Roles() {
		v := n.model.Data(ix, role)
		if v.Kind == qml.SpecValueInvalid {
			v = qml.SpecValue{Kind: qml.SpecValueString}
		}
		locals["model."+role] = v
	}
	var st style.Style
	for _, prop := range n.template.Props {
		v, err := n.eval(prop.Value, locals)
		if err != nil {
			if n.sink != nil {
				n.sink(err)
			}
			continue
		}
		switch prop.Name {
		case "text":
			label = v.Raw
		case "color":
			color, err := colorOf(v)
			if err != nil {
				if n.sink != nil {
					n.sink(err)
				}
				continue
			}
			st = st.Foreground(color)
		}
	}
	return label, st
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
		label, st := n.present(ix)
		if st != (style.Style{}) {
			opts = append(opts, widget.WithNodeStyle(st))
		}
		node := widget.NewTreeNode(n.model.Key(ix), label, opts...)
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
		n.continueReveal(path)
		return
	}
	// Children changed under a row: an open one takes them now, what was open
	// below it opening again and the cursor staying on its row (Tree.Reload);
	// a closed one drops what it cached and loads on the next open.
	n.tree.ReloadNode(node)
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
			n.continueReveal(n.pathOf(ix))
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
	tui.SubscribeScoped(ctx, func(ev widget.SelectionChangedEvent) {
		if ev.Owner != n.tree.NodeID() || n.model == nil {
			return
		}
		// The row under the cursor NOW: the event was queued, and the tree
		// may have moved since. A repeat of the last row sends nothing.
		node, ok := n.tree.Selected()
		if !ok {
			return
		}
		ix, ok := n.at[node]
		if !ok {
			return
		}
		if path := n.pathOf(ix); path != n.lastMoved {
			n.lastMoved = path
			n.moved(indexValue(ix))
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
	restyle: func(c tui.Component, p palette) {
		n := c.(*treeViewNode)
		n.tree.ResetStyles(p.viewStyles())
		n.normal = style.Default()
		if text, ok := p[roleText]; ok {
			n.normal = text
		}
		for node, ix := range n.at {
			label, st := n.present(ix)
			node.SetLabel(label)
			node.SetStyle(st)
		}
	},
	Ctor: []string{"textRole", "badgeRole", "delegate"},
	Setters: map[string]Setter{
		"model": func(c tui.Component, v qml.SpecValue) error {
			m, err := treeModelOf(v)
			if err != nil {
				return err
			}
			n := c.(*treeViewNode)
			if err := n.checkTemplate(m); err != nil {
				return err
			}
			n.setModel(m)
			return nil
		},
	},
	Methods: map[string]Method{
		"toggleExpanded": func(c tui.Component, args []qml.SpecValue) error {
			n, ok := c.(*treeViewNode)
			if !ok {
				return fmt.Errorf("not a TreeView")
			}
			if len(args) != 1 {
				return fmt.Errorf("toggleExpanded takes a row's index, and was given %d arguments", len(args))
			}
			ix, ok := args[0].Obj.(Index)
			if !ok {
				return fmt.Errorf("toggleExpanded takes a row's index (a signal's index), not %s", args[0].Raw)
			}
			return n.toggleExpanded(ix)
		},
		"setCurrentIndex": func(c tui.Component, args []qml.SpecValue) error {
			n, ok := c.(*treeViewNode)
			if !ok {
				return fmt.Errorf("not a TreeView")
			}
			if len(args) != 1 {
				return fmt.Errorf("setCurrentIndex takes a row's index, and was given %d arguments", len(args))
			}
			ix, ok := args[0].Obj.(Index)
			if !ok {
				return fmt.Errorf("setCurrentIndex takes a row's index, not %s", args[0].Raw)
			}
			return n.setCurrentIndex(ix)
		},
	},
	Signals:   map[string][]string{"activated": {"index"}, "expanded": {"index"}, "currentIndexChanged": {"index"}},
	Destroyed: func(c tui.Component) { c.(*treeViewNode).release() },
}

// toggleExpanded opens or closes the row at ix — Qt's TreeView.toggleExpanded,
// by the row's Index. A row the view does not show — under a closed parent, or
// gone — is refused.
//
// An Index is a POSITION, as Qt's QModelIndex is: it names whatever row is at
// that place when it is used. Use it as a signal gives it — a handler passing
// its `index` on — not after the model has changed; a host that keeps rows
// across changes keeps their keys.
func (n *treeViewNode) toggleExpanded(ix Index) error {
	if n.model == nil {
		return fmt.Errorf("toggleExpanded: the TreeView has no model")
	}
	node, ok := n.byPath[n.pathOf(ix)]
	if !ok || !slices.Contains(n.tree.VisibleRows(), node) {
		return fmt.Errorf("toggleExpanded: no row at that index is shown")
	}
	n.tree.ToggleExpanded(node)
	return nil
}

// setCurrentIndex puts the cursor on the row at ix, opening its closed
// ancestors first: QAbstractItemView.setCurrentIndex together with Qt's
// TreeView.expandToIndex. The row itself is not opened.
//
// Every ancestor's children must already be loaded in the model (RowCount > 0
// and nothing more to fetch); the view does not fetch to reach a row, so a
// host that wants a deeper row loads it first. A row that is not in the model
// is refused.
//
// Opening an ancestor the view has not shown before settles on a later turn of
// the loop, so a deep row is reached one level per turn, and the cursor passes
// through the ancestors on the way (each move emits currentIndexChanged). The
// reveal gives up, reporting through the error sink, if the rows change so that
// the row is no longer reachable. A later setCurrentIndex replaces one in
// flight.
func (n *treeViewNode) setCurrentIndex(ix Index) error {
	if n.model == nil {
		return fmt.Errorf("setCurrentIndex: the TreeView has no model")
	}
	var chain []Index
	for p := &ix; p != nil; p = p.Parent {
		chain = append([]Index{*p}, chain...)
	}
	keys := make([]string, len(chain))
	for i, at := range chain {
		var parent *Index
		if i > 0 {
			parent = &chain[i-1]
			if n.model.CanFetchMore(*parent) {
				return fmt.Errorf("setCurrentIndex: a parent of that row has children still to load")
			}
		}
		if at.Row < 0 || at.Row >= n.model.RowCount(parent) {
			return fmt.Errorf("setCurrentIndex: no row at that index")
		}
		keys[i] = n.model.Key(at)
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strconv.Itoa(len(k)) + ":" + k
	}
	n.reveal = &treeReveal{parts: parts, keys: keys, rounds: len(keys)}
	n.continueReveal("")
	return nil
}

// continueReveal takes a setCurrentIndex in flight one step. It opens the
// row's ancestors as far as they are shown; when the row itself is shown it
// moves the cursor there and finishes.
//
// changed is the path whose children were just set ("" for the first step).
// Only a change on the row's own path advances the reveal or spends one of its
// rounds, so an unrelated branch loading later cannot move the cursor. A level
// whose rows are shown but do not include the next key ends the reveal at
// once: the row is gone.
func (n *treeViewNode) continueReveal(changed string) {
	r := n.reveal
	if r == nil {
		return
	}
	last := len(r.keys) - 1
	if changed != "" && changed != r.path(last) && !strings.HasPrefix(r.path(last), changed+"/") {
		return
	}
	if last > 0 {
		n.tree.ExpandPath(r.keys[:last]...) // opens only the ancestors
	}
	for depth := 0; depth <= last; depth++ {
		node, shown := n.byPath[r.path(depth)]
		if !shown {
			if depth == 0 || n.childrenShown(r.path(depth-1)) {
				n.giveUpReveal()
			}
			break // an ancestor is still opening
		}
		if depth < last {
			continue
		}
		for i, row := range n.tree.VisibleRows() {
			if row == node {
				n.reveal = nil
				n.tree.SetCursor(i)
				return
			}
		}
	}
	if n.reveal == nil {
		return
	}
	if r.rounds--; r.rounds < 0 {
		n.giveUpReveal()
	}
}

// childrenShown reports whether the view holds any row under path.
func (n *treeViewNode) childrenShown(path string) bool {
	prefix := path + "/"
	for p := range n.byPath {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func (n *treeViewNode) giveUpReveal() {
	n.reveal = nil
	if n.sink != nil {
		n.sink(fmt.Errorf("TreeView.setCurrentIndex: the row is no longer reachable"))
	}
}
