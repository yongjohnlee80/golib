package widget

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TreeNode is one explorer entry (ADR-0008 §2.2). Children are unknown
// until the node is expanded: the Tree publishes ExpandRequestEvent with a
// generation token and the application answers with SetChildren or
// SetLoadError carrying that token — every outcome settles the spinner,
// stale generations are inert.
//
// Ownership (r3): attaching a node — via SetRoots or SetChildren — stamps
// it and its subtree with the owning Tree. Attaching an already-owned node
// panics (duplicate pointers, cross-tree reuse, ancestor adoption are all
// impossible by construction). Detaching (being replaced out, or Reset's
// discard) releases the subtree for re-attachment.
type TreeNode struct {
	id    string
	label string
	leaf  bool
	badge string
	st    style.Style
	hasSt bool

	owner    *Tree
	parent   *TreeNode
	children []*TreeNode

	expanded bool
	loaded   bool
	loading  bool
	gen      uint64 // pending load generation; 0 = none
	loadErr  string
}

// NodeOption customizes a TreeNode under construction.
type NodeOption func(*TreeNode)

// WithLeaf marks the node never-expandable (activation only).
func WithLeaf() NodeOption {
	return func(n *TreeNode) { n.leaf = true }
}

// WithBadge sets a trailing annotation (e.g. "view", "fn", row counts).
func WithBadge(b string) NodeOption {
	return func(n *TreeNode) { n.badge = b }
}

// WithNodeStyle overrides the node's label style.
func WithNodeStyle(st style.Style) NodeOption {
	return func(n *TreeNode) { n.st = st; n.hasSt = true }
}

// NewTreeNode builds an unattached node. id must be unique among the
// siblings it is eventually attached with.
func NewTreeNode(id, label string, opts ...NodeOption) *TreeNode {
	if id == "" {
		panic("widget: NewTreeNode: empty id")
	}
	n := &TreeNode{id: id, label: label}
	for _, o := range opts {
		if o != nil {
			o(n)
		}
	}
	return n
}

// ID returns the node's identifier.
func (n *TreeNode) ID() string { return n.id }

// SetLabel updates the display label (dirties the owning tree).
func (n *TreeNode) SetLabel(label string) {
	n.label = label
	n.dirty()
}

// SetBadge updates the trailing annotation.
func (n *TreeNode) SetBadge(b string) {
	n.badge = b
	n.dirty()
}

func (n *TreeNode) dirty() {
	if n.owner != nil {
		n.owner.MarkDirty()
	}
}

// structureChanged marks the owner dirty AND reconciles its cursor
// synchronously — input handling must never depend on a later render to
// make model state valid (MF3).
func (n *TreeNode) structureChanged() {
	if n.owner != nil {
		n.owner.reconcile()
		n.owner.MarkDirty()
	}
}

// SetChildren supplies the node's children.
//
//   - On an UNOWNED node, gen must be 0: static pre-assembly before the
//     subtree is attached to a Tree.
//   - On an owned node, gen must match the pending generation from
//     ExpandRequestEvent; anything else — a superseded request, a result
//     arriving after collapse/Reset/SetRoots, or a detached node — is
//     ignored entirely (stale results never overwrite newer state).
//
// Duplicate child IDs and already-owned children panic (construction bug).
// Empty kids marks a loaded, leaf-like node.
func (n *TreeNode) SetChildren(gen uint64, kids []*TreeNode) {
	if n.owner == nil {
		if gen != 0 {
			return // late result for a detached node: inert
		}
	} else if gen == 0 || gen != n.gen {
		return // stale or unsolicited: inert
	}
	// Validate the ENTIRE incoming forest before touching anything —
	// including that neither the receiver nor any of its ancestors appears
	// in it (receiver cycles, r2) — and copy the slice so the caller
	// cannot mutate the committed graph.
	forbidden := make(map[*TreeNode]struct{})
	for a := n; a != nil; a = a.parent {
		forbidden[a] = struct{}{}
	}
	preflightForest(kids, forbidden)
	committed := append([]*TreeNode(nil), kids...)
	for _, old := range n.children {
		old.release()
	}
	n.children = committed
	for _, k := range committed {
		k.parent = n
		if n.owner != nil {
			n.owner.adopt(k)
		}
	}
	n.loaded = true
	n.loading = false
	n.loadErr = ""
	n.gen = 0
	n.structureChanged()
}

// SetLoadError settles a pending load into an error badge: the node
// collapses back to the unloaded state, so the next expand fires a NEW
// generation (retry is user-driven). Stale generations are inert.
func (n *TreeNode) SetLoadError(gen uint64, msg string) {
	if n.owner == nil || gen == 0 || gen != n.gen {
		return
	}
	n.loading = false
	n.loaded = false
	n.expanded = false
	n.loadErr = msg
	n.gen = 0
	n.structureChanged()
}

// Reset discards loaded children and any in-flight generation (refresh).
// The still-attached receiver remains owned; the discarded descendants are
// released for re-attachment.
func (n *TreeNode) Reset() {
	for _, old := range n.children {
		old.release()
	}
	n.children = nil
	n.loaded = false
	n.loading = false
	n.expanded = false
	n.loadErr = ""
	n.gen = 0
	n.structureChanged()
}

// release detaches a subtree: ownership clears recursively but ONLY the
// detached root's external parent edge is cut — the subtree's internal
// ancestry survives for re-attachment (r3 release precision + MF2).
func (n *TreeNode) release() {
	n.parent = nil
	n.releaseOwned()
}

func (n *TreeNode) releaseOwned() {
	n.owner = nil
	n.gen = 0
	n.loading = false
	for _, c := range n.children {
		c.releaseOwned()
	}
}

// preflightForest validates an incoming forest COMPLETELY before any
// mutation (MF: attachment must never be destructive-then-panic): nil
// nodes, duplicate pointers anywhere in the forest (which also covers
// shared descendants and cycles), owned nodes anywhere, duplicate sibling
// IDs at every level, inconsistent internal parent links, and any node in
// forbidden (the receiver and its ancestors — n.SetChildren(0, n) and
// longer receiver cycles reject BEFORE commit, r2) all reject.
func preflightForest(kids []*TreeNode, forbidden map[*TreeNode]struct{}) {
	visited := make(map[*TreeNode]struct{})
	var walk func(n *TreeNode, parent *TreeNode)
	walk = func(n *TreeNode, parent *TreeNode) {
		if n == nil {
			panic("widget: nil tree node")
		}
		if _, dup := visited[n]; dup {
			panic(fmt.Sprintf("widget: tree node %q appears more than once in the incoming forest", n.id))
		}
		if _, bad := forbidden[n]; bad {
			panic(fmt.Sprintf("widget: tree node %q would become its own descendant", n.id))
		}
		visited[n] = struct{}{}
		if n.owner != nil {
			panic(fmt.Sprintf("widget: tree node %q is already attached to a tree", n.id))
		}
		if parent != nil && n.parent != nil && n.parent != parent {
			panic(fmt.Sprintf("widget: tree node %q carries an inconsistent parent link", n.id))
		}
		validateSiblingIDs(n.children)
		for _, c := range n.children {
			walk(c, n)
		}
	}
	validateSiblingIDs(kids)
	for _, k := range kids {
		if k != nil && k.parent != nil {
			panic(fmt.Sprintf("widget: tree node %q is still linked inside another assembly", k.id))
		}
		walk(k, nil)
	}
}

// validateSiblingIDs panics on duplicate IDs within one sibling set.
func validateSiblingIDs(kids []*TreeNode) {
	seen := make(map[string]struct{}, len(kids))
	for _, k := range kids {
		if k == nil {
			panic("widget: nil tree node")
		}
		if _, dup := seen[k.id]; dup {
			panic(fmt.Sprintf("widget: duplicate sibling tree-node id %q", k.id))
		}
		seen[k.id] = struct{}{}
	}
}

// ExpandRequestEvent fires once per unloaded expand, carrying the request
// generation the application must echo into SetChildren / SetLoadError.
type ExpandRequestEvent struct {
	Owner tui.NodeID
	Node  *TreeNode
	Gen   uint64
}

// CollapseEvent fires when a node collapses.
type CollapseEvent struct {
	Owner tui.NodeID
	Node  *TreeNode
}

// Tree is the lazy expandable hierarchy (ADR-0008 §2.2): flattened
// virtualized rows over owner-stamped nodes, vim-style navigation
// (j/k/l/h + arrows + Enter), spinner/error badges, and the generation
// lifecycle above.
type Tree struct {
	Base
	roots  []*TreeNode
	cursor int
	top    int
	w, h   int
	indent int
	styles ListStyles
	genSeq uint64
}

var _ tui.Focusable = (*Tree)(nil)

// TreeOption customizes a Tree under construction.
type TreeOption func(*Tree)

// WithRoots seeds the root nodes.
func WithRoots(roots ...*TreeNode) TreeOption {
	return func(t *Tree) { t.SetRoots(roots...) }
}

// WithTreeStyles overrides the row styles (List slots).
func WithTreeStyles(st ListStyles) TreeOption {
	return func(t *Tree) {
		t.styles = ListStyles{
			Row:            st.Row.Inherit(t.styles.Row),
			CursorRow:      st.CursorRow.Inherit(t.styles.CursorRow),
			SelectedRow:    st.SelectedRow.Inherit(t.styles.SelectedRow),
			CursorSelected: st.CursorSelected.Inherit(t.styles.CursorSelected),
		}
	}
}

// WithIndent sets the per-depth indent (default 2; non-positive panics).
func WithIndent(n int) TreeOption {
	if n <= 0 {
		panic(fmt.Sprintf("widget: WithIndent: %d is not positive", n))
	}
	return func(t *Tree) { t.indent = n }
}

// NewTree builds an empty tree.
func NewTree(opts ...TreeOption) *Tree {
	t := &Tree{
		indent: 2,
		styles: ListStyles{
			CursorRow: style.New().Background(style.TokenPrimary).Foreground(style.TokenTextOnPrimary),
		},
	}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	return t
}

// AcceptsFocus implements tui.Focusable.
func (t *Tree) AcceptsFocus() bool { return true }

// adopt stamps a subtree with this tree as owner (preflighted callers only)
// and re-stamps internal parent links so a released-then-reattached
// subtree navigates correctly.
func (t *Tree) adopt(n *TreeNode) {
	if n.owner != nil {
		panic(fmt.Sprintf("widget: tree node %q is already attached to a tree", n.id))
	}
	n.owner = t
	for _, c := range n.children {
		c.parent = n
		t.adopt(c)
	}
}

// SetRoots replaces the root set: removed subtrees are released, incoming
// roots adopted (owned roots and duplicate IDs panic), every outstanding
// generation on the old roots is invalidated by the release.
func (t *Tree) SetRoots(roots ...*TreeNode) {
	preflightForest(roots, nil)
	committed := append([]*TreeNode(nil), roots...)
	for _, old := range t.roots {
		old.release()
	}
	t.roots = committed
	for _, r := range committed {
		r.parent = nil
		t.adopt(r)
	}
	t.cursor, t.top = 0, 0
	t.MarkDirty()
}

// Selected returns the node under the cursor.
func (t *Tree) Selected() (*TreeNode, bool) {
	rows := t.flatten()
	if t.cursor < 0 || t.cursor >= len(rows) {
		return nil, false
	}
	return rows[t.cursor].node, true
}

// ExpandPath reveals (and moves the cursor to) the node addressed by ids,
// resolved level by level from the roots. Unloaded nodes on the path stop
// the walk after requesting their expansion — call again once loaded.
func (t *Tree) ExpandPath(ids ...string) {
	level := t.roots
	var target *TreeNode
	for _, id := range ids {
		var found *TreeNode
		for _, n := range level {
			if n.id == id {
				found = n
				break
			}
		}
		if found == nil {
			return
		}
		target = found
		if !found.leaf && !found.expanded {
			t.expandNode(found)
		}
		if !found.loaded {
			break // async load in flight; caller re-runs when it settles
		}
		level = found.children
	}
	if target != nil {
		rows := t.flatten()
		for i, r := range rows {
			if r.node == target {
				t.cursor = i
				break
			}
		}
	}
	t.ensureVisible()
	t.MarkDirty()
}

// --- flattened view ---------------------------------------------------------

type treeRow struct {
	node  *TreeNode
	depth int
}

func (t *Tree) flatten() []treeRow {
	var out []treeRow
	var walk func(n *TreeNode, depth int)
	walk = func(n *TreeNode, depth int) {
		out = append(out, treeRow{node: n, depth: depth})
		if n.expanded && n.loaded {
			for _, c := range n.children {
				walk(c, depth+1)
			}
		}
	}
	for _, r := range t.roots {
		walk(r, 0)
	}
	return out
}

// --- interaction --------------------------------------------------------------

// expandNode opens a node: loaded → show children; unloaded → fire ONE
// ExpandRequestEvent with a fresh generation and show the spinner badge.
func (t *Tree) expandNode(n *TreeNode) {
	if n.leaf {
		return
	}
	if n.loaded {
		n.expanded = true
		t.MarkDirty()
		return
	}
	if n.loading {
		return // request already in flight
	}
	t.genSeq++
	n.gen = t.genSeq
	n.loading = true
	n.loadErr = ""
	n.expanded = true // renders the spinner badge until the result settles
	t.MarkDirty()
	t.publish(ExpandRequestEvent{Owner: t.NodeID(), Node: n, Gen: n.gen})
}

// collapseNode closes a node; collapsing a loading node invalidates its
// generation (the eventual result is inert).
func (t *Tree) collapseNode(n *TreeNode) {
	if !n.expanded && !n.loading {
		return
	}
	n.expanded = false
	if n.loading {
		n.loading = false
		n.gen = 0
	}
	t.MarkDirty()
	t.publish(CollapseEvent{Owner: t.NodeID(), Node: n})
}

// HandleEvent implements the navigation contract.
func (t *Tree) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		return t.handleKey(e)
	case tui.MouseEvent:
		return t.handleMouse(e)
	}
	return false
}

func (t *Tree) handleKey(e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	rows := t.flatten()
	if len(rows) == 0 {
		return false
	}
	code := e.Code
	if e.Text != "" && e.Mods&nonTextMods == 0 {
		code = []rune(e.Text)[0]
	}
	t.cursor = max(0, min(t.cursor, len(rows)-1)) // never index a stale cursor
	cur := rows[t.cursor]
	switch code {
	case 'j', tui.KeyDown:
		t.moveCursor(1, len(rows))
		return true
	case 'k', tui.KeyUp:
		t.moveCursor(-1, len(rows))
		return true
	case tui.KeyPageDown:
		t.moveCursor(max(t.h, 1), len(rows))
		return true
	case tui.KeyPageUp:
		t.moveCursor(-max(t.h, 1), len(rows))
		return true
	case tui.KeyHome:
		t.cursor = 0
		t.ensureVisible()
		t.MarkDirty()
		return true
	case tui.KeyEnd:
		t.cursor = len(rows) - 1
		t.ensureVisible()
		t.MarkDirty()
		return true
	case 'l', tui.KeyRight:
		if cur.node.leaf {
			t.publish(ActivateEvent{Owner: t.NodeID(), Index: t.cursor})
			return true
		}
		if cur.node.expanded && cur.node.loaded && len(cur.node.children) > 0 {
			t.moveCursor(1, len(rows)) // into the first child
			return true
		}
		t.expandNode(cur.node)
		return true
	case tui.KeyEnter:
		if cur.node.leaf {
			t.publish(ActivateEvent{Owner: t.NodeID(), Index: t.cursor})
			return true
		}
		if cur.node.expanded {
			t.collapseNode(cur.node)
		} else {
			t.expandNode(cur.node)
		}
		return true
	case 'h', tui.KeyLeft:
		if cur.node.expanded || cur.node.loading {
			t.collapseNode(cur.node)
			return true
		}
		if cur.node.parent != nil {
			for i, r := range rows {
				if r.node == cur.node.parent {
					t.cursor = i
					break
				}
			}
			t.ensureVisible()
			t.MarkDirty()
		}
		return true
	}
	return false
}

func (t *Tree) handleMouse(e tui.MouseEvent) bool {
	if e.Kind != tui.MousePress || e.Button != tui.MouseLeft {
		return false
	}
	rows := t.flatten()
	idx := t.top + e.Y
	if idx < 0 || idx >= len(rows) {
		return false
	}
	r := rows[idx]
	t.cursor = idx
	// A click on the expander glyph toggles; elsewhere selects.
	if e.X >= r.depth*t.indent && e.X < r.depth*t.indent+2 && !r.node.leaf {
		if r.node.expanded {
			t.collapseNode(r.node)
		} else {
			t.expandNode(r.node)
		}
	}
	t.ensureVisible()
	t.MarkDirty()
	return true
}

// reconcile clamps cursor/top into the current flattened rows.
func (t *Tree) reconcile() {
	rows := t.flatten()
	t.cursor = max(0, min(t.cursor, len(rows)-1))
	t.ensureVisible()
}

func (t *Tree) moveCursor(delta, total int) {
	t.cursor = max(0, min(t.cursor+delta, total-1))
	t.ensureVisible()
	t.MarkDirty()
}

func (t *Tree) ensureVisible() {
	if t.h <= 0 {
		return
	}
	if t.cursor < t.top {
		t.top = t.cursor
	}
	if t.cursor >= t.top+t.h {
		t.top = t.cursor - t.h + 1
	}
	t.top = max(0, t.top)
}

// --- layout & render -----------------------------------------------------------

// Layout is greedy on both axes.
func (t *Tree) Layout(c tui.Constraints) tui.Size {
	t.w = boundedMax(c.MaxW, max(c.MinW, 1))
	t.h = boundedMax(c.MaxH, max(c.MinH, 1))
	t.ensureVisible()
	return c.Constrain(tui.Size{W: t.w, H: t.h})
}

// Render paints the visible flattened rows.
func (t *Tree) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	rows := t.flatten()
	if t.cursor >= len(rows) {
		t.cursor = max(0, len(rows)-1)
	}
	w := sz.W
	scrollable := len(rows) > sz.H
	if scrollable {
		w--
	}
	for y := 0; y < sz.H; y++ {
		idx := t.top + y
		if idx >= len(rows) {
			break
		}
		r := rows[idx]
		st := t.styles.Row
		if r.node.hasSt {
			st = r.node.st.Inherit(st)
		}
		if idx == t.cursor && t.focused() {
			st = t.styles.CursorRow.Inherit(st)
			s.Fill(tui.Rect{X: 0, Y: y, W: w, H: 1}, " ", st)
		}
		line := t.rowText(r)
		s.SetCell(0, y, "", st) // ensure the row participates even when empty
		drawText(s, 0, y, truncate(line, w, s.StringWidth), st)
	}
	if scrollable {
		paintScrollIndicator(s, sz.W-1, sz.H, t.top, len(rows))
	}
}

// rowText renders one row: indent + expander + label + badge.
func (t *Tree) rowText(r treeRow) string {
	var expander string
	switch {
	case r.node.leaf:
		expander = "· "
	case r.node.loading:
		expander = "◌ "
	case r.node.expanded && r.node.loaded:
		expander = "▾ "
	default:
		expander = "▸ "
	}
	var badge string
	switch {
	case r.node.loading:
		badge = " …"
	case r.node.loadErr != "":
		badge = " ⚠ " + r.node.loadErr
	case r.node.badge != "":
		badge = " " + r.node.badge
	}
	return strings.Repeat(" ", r.depth*t.indent) + expander + r.node.label + badge
}
