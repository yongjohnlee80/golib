package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE MENU'S OWN RESOLVER AND STATE MACHINE.
//
// Everything here exists because a model row is not a node. The runtime's
// recogniser arms a COMPONENT; a Menu needs to arm a ROW, which the runtime
// cannot name. So the Menu resolves its own pointer and key events into the
// actions in menuactions.go, and handles them in one place — and because both
// input kinds end at the same MenuActivateAction, keyboard and mouse cannot
// drift apart.

// resolve is the Menu's default resolver: pointer events mapped through the
// regions of the last committed layout, plus the keyboard bindings.
func (m *Menu) resolve(ev tui.Event) (tui.Action, bool) {
	return m.resolveIn(ev, m.rowRects)
}

// resolveIn is the resolver for ONE level: the Menu's own rows, or an open
// popup's. Which rect map to map through is the only thing that differs between
// them, so it is a parameter rather than a second resolver that could drift.
func (m *Menu) resolveIn(ev tui.Event, rects map[ItemID]tui.Rect) (tui.Action, bool) {
	switch e := ev.(type) {
	case tui.MouseEvent:
		return m.resolveMouse(e, rects)
	case tui.KeyEvent:
		return m.resolveKey(e)
	}
	return nil, false
}

// resolveMouse maps owner-local coordinates through the last committed row
// rects. It never consults the model for geometry: the rects ARE what was
// painted, so a hit cannot disagree with what the user aimed at.
func (m *Menu) resolveMouse(e tui.MouseEvent, rects map[ItemID]tui.Rect) (tui.Action, bool) {
	if e.Button != tui.MouseLeft && e.Kind != tui.MouseMotion {
		// A non-primary release during a primary gesture is ignored entirely:
		// the gesture is still running and its capture is still owned.
		return nil, false
	}
	row, hit := m.rowAt(e.X, e.Y, rects)

	switch e.Kind {
	case tui.MousePress:
		if !hit {
			return nil, false
		}
		return MenuArmAction{ItemID: row}, true
	case tui.MouseMotion:
		if m.pressed == "" {
			return nil, false // not a drag: hover does not select
		}
		if !hit {
			return MenuDisarmAction{}, true
		}
		return MenuSelectAction{ItemID: row}, true
	case tui.MouseRelease:
		if m.pressed == "" {
			return nil, false
		}
		if hit && row == m.pressed && m.armed {
			return MenuActivateAction{ItemID: row}, true
		}
		// Released anywhere else: end the gesture, activate nothing, and leave
		// the open levels exactly as they were.
		return MenuCancelAction{}, true
	}
	return nil, false
}

// resolveKey binds the menu's keyboard vocabulary.
//
// Arrow direction follows the LAYOUT: a horizontal bar steps with Left/Right and
// opens downward, a vertical menu steps with Up/Down and opens to the side. The
// alternative — one fixed mapping — makes a menu bar behave like a list rotated
// ninety degrees, which every user notices immediately.
func (m *Menu) resolveKey(e tui.KeyEvent) (tui.Action, bool) {
	if e.Kind == tui.KeyRelease || e.Mods.Chord() != 0 {
		return nil, false
	}
	if m.horizontal {
		if a, ok := m.resolveBarKey(e); ok {
			return a, ok
		}
	} else if a, ok := m.resolveColumnKey(e); ok {
		return a, ok
	}
	switch e.Code {
	case tui.KeyEscape:
		return MenuCloseAction{All: true}, true
	case tui.KeyEnter, ' ':
		if id, ok := m.Selected(); ok {
			return MenuActivateAction{ItemID: id}, true
		}
		return nil, false
	}
	// A mnemonic activates its row directly, from wherever the selection is.
	// Code carries the printable rune for an ordinary key, so there is no
	// separate "is this text" flag to consult.
	if id, ok := m.mnemonic(e.Code); ok {
		return MenuActivateAction{ItemID: id}, true
	}
	return nil, false
}

// resolveBarKey is the arrow vocabulary of a horizontal MENU BAR.
//
// THE TWO AXES MEAN DIFFERENT THINGS, and keeping them separate is the whole
// point. Left and Right walk the BAR — always, whatever is open and whatever
// kind of row the selection is on. Up and Down walk the open dropdown.
//
// Right used to open a submenu when the selection was on one, which made the
// bar unreachable from inside a category whose first row cascades: in a menu
// whose Option holds "Keymaps", Right opened Keymaps and there was no way to
// reach Help at all. One key cannot both walk the bar and descend a cascade;
// Enter descends, and Up comes back out.
//
// Up at the first row CLOSES the level rather than wrapping to the last. A
// cascade is a stack, and the way out of a stack is back the way you came —
// wrapping to the bottom of a four-row dropdown when the user is trying to get
// back to the bar is a small maze.
func (m *Menu) resolveBarKey(e tui.KeyEvent) (tui.Action, bool) {
	switch e.Code {
	case tui.KeyRight:
		// A SUBMENU ROW STILL DESCENDS. Right is how you reach the keymaps
		// under Option, and taking that away to free the key for the bar would
		// trade one unreachable place for another.
		//
		// What makes both possible is that Up now closes a level: from a
		// category whose only row cascades, the way to the next category is Up
		// and then Right, rather than being stuck inside the cascade with no
		// exit — which is what it was before.
		// ONLY INSIDE A LEVEL. On the bar itself every row is a submenu, so a
		// descend-if-submenu rule there would make Right open the current
		// category instead of stepping to the next one — which is Down's job
		// and was briefly Right's too, breaking the bar entirely.
		if len(m.levels) > 0 {
			if id, ok := m.Selected(); ok {
				if it := findItem(m.items, id); it != nil && it.Kind == ItemKindSubmenu {
					return MenuActivateAction{ItemID: id}, true
				}
			}
		}
		return m.barStep(+1)
	case tui.KeyLeft:
		// Inside a cascade, back out one level — the mirror of Right
		// descending. At the first level there is nothing to back out of, so
		// the key walks the bar.
		if len(m.levels) > 1 {
			return MenuCloseAction{}, true
		}
		return m.barStep(-1)
	case tui.KeyDown:
		if len(m.levels) == 0 {
			// Down opens the category the selection is on, which is the only
			// way into the cascade from the bar besides Enter.
			if id, ok := m.Selected(); ok {
				if it := findItem(m.items, id); it != nil && it.Kind == ItemKindSubmenu {
					return MenuActivateAction{ItemID: id}, true
				}
			}
			return nil, false
		}
		return MenuSelectAction{ItemID: m.neighbour(+1)}, true
	case tui.KeyUp:
		if len(m.levels) == 0 {
			return nil, false // nothing above the bar
		}
		if m.atLevelTop() {
			return MenuCloseAction{}, true
		}
		return MenuSelectAction{ItemID: m.neighbour(-1)}, true
	}
	return nil, false
}

// resolveColumnKey is the arrow vocabulary of a VERTICAL menu, which has no bar
// to walk: Up and Down move, Right descends into a submenu, Left comes back.
//
// Unchanged, and deliberately not merged with the bar's. A column has one axis
// of travel and cascades sideways; a bar has two axes that mean different
// things. Forcing one table to serve both is what produced a Right key that
// sometimes walked and sometimes descended.
func (m *Menu) resolveColumnKey(e tui.KeyEvent) (tui.Action, bool) {
	switch e.Code {
	case tui.KeyDown:
		return MenuSelectAction{ItemID: m.neighbour(+1)}, true
	case tui.KeyUp:
		return MenuSelectAction{ItemID: m.neighbour(-1)}, true
	case tui.KeyRight:
		if id, ok := m.Selected(); ok {
			if it := findItem(m.items, id); it != nil && it.Kind == ItemKindSubmenu {
				return MenuActivateAction{ItemID: id}, true
			}
		}
		return nil, false
	case tui.KeyLeft:
		if len(m.levels) > 0 {
			return MenuCloseAction{}, true
		}
		return nil, false
	}
	return nil, false
}

// atLevelTop reports whether the selection is the FIRST selectable row of the
// level currently on screen — the row where Up stops moving and starts closing.
func (m *Menu) atLevelTop() bool {
	for _, it := range m.currentLevelItems() {
		if it.selectable() {
			return it.ID == m.selected
		}
	}
	return false
}

// barStep walks the bar by delta, carrying an open dropdown with it.
//
// With a level open the step ACTIVATES the neighbouring category, which both
// opens its dropdown and — since opening truncates to the row's own depth —
// closes the one being left. With nothing open it is an ordinary selection
// move along the bar.
func (m *Menu) barStep(delta int) (tui.Action, bool) {
	if len(m.levels) > 0 {
		if id, ok := m.barNeighbour(delta); ok {
			return MenuActivateAction{ItemID: id}, true
		}
		return nil, false
	}
	return MenuSelectAction{ItemID: m.neighbour(delta)}, true
}

// barNeighbour is the next selectable ROOT row, for a horizontal bar with a
// level open — the category Left and Right walk to while a dropdown is showing.
//
// Only for a bar with something open. A vertical menu has no "along the bar" to
// walk, and a bar with nothing open already steps its root rows with the same
// keys through the ordinary neighbour path.
func (m *Menu) barNeighbour(delta int) (ItemID, bool) {
	if !m.horizontal || len(m.levels) == 0 {
		return "", false
	}
	// Which root row the open cascade belongs to, regardless of how deep the
	// selection currently is.
	root := m.levels[0].parent
	idx := -1
	for i := range m.items {
		if m.items[i].ID == root {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	n := len(m.items)
	for stepN := 1; stepN <= n; stepN++ {
		j := ((idx+delta*stepN)%n + n) % n
		if m.items[j].selectable() && m.items[j].Kind == ItemKindSubmenu {
			return m.items[j].ID, true
		}
	}
	return "", false
}

// neighbour is the next selectable row in the current level, wrapping. Returns
// the current selection when the level has nothing else to move to, so a
// single-row menu does not emit a change event on every keypress.
func (m *Menu) neighbour(delta int) ItemID {
	level := m.currentLevelItems()
	idx := -1
	for i := range level {
		if level[i].ID == m.selected {
			idx = i
			break
		}
	}
	n := len(level)
	for step := 1; step <= n; step++ {
		j := ((idx+delta*step)%n + n) % n
		if level[j].selectable() {
			return level[j].ID
		}
	}
	return m.selected
}

// mnemonic finds the row whose Hotkey matches, in the current level only —
// a mnemonic belongs to the level on screen, not to rows the user cannot see.
func (m *Menu) mnemonic(r rune) (ItemID, bool) {
	level := m.currentLevelItems()
	for i := range level {
		if level[i].selectable() && level[i].Hotkey != 0 && eqFold(level[i].Hotkey, r) {
			return level[i].ID, true
		}
	}
	return "", false
}

// eqFold compares two mnemonic runes case-insensitively for ASCII, which is the
// range a keyboard mnemonic is drawn from.
func eqFold(a, b rune) bool {
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}
	return lower(a) == lower(b)
}

// rowAt maps owner-local coordinates to a SELECTABLE row. A separator, a
// disabled row or a hidden one is not a hit: they cannot be armed, so reporting
// them would make every caller re-check.
func (m *Menu) rowAt(x, y int, rects map[ItemID]tui.Rect) (ItemID, bool) {
	for id, r := range rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			if it := findItem(m.items, id); it != nil && it.selectable() {
				return id, true
			}
			return "", false
		}
	}
	return "", false
}

// HandleAction is the one place the menu's vocabulary is interpreted.
func (m *Menu) HandleAction(inv tui.ActionInvocation) bool {
	return m.handleIn(inv, m.Context())
}

// handleIn is the machine, told which node's handler is running.
//
// The capturing node matters: a press can land on the root Menu or on any open
// level, and only the node that took a capture may release it. Passing the
// context in is what lets one machine serve every level rather than each level
// growing its own copy.
func (m *Menu) handleIn(inv tui.ActionInvocation, ctx *tui.Context) bool {
	switch a := inv.Action.(type) {
	case MenuArmAction:
		return m.arm(a.ItemID, ctx)
	case MenuSelectAction:
		return m.selectRow(a.ItemID)
	case MenuDisarmAction:
		return m.disarm()
	case MenuCancelAction:
		return m.cancelGesture()
	case MenuCloseAction:
		return m.closeAction(a.All)
	case MenuActivateAction:
		return m.activate(a.ItemID, inv)
	}
	return false
}

// arm presses a row: select it, remember it, and take the pointer so motion and
// the release keep arriving here even when they leave the menu's own rect.
func (m *Menu) arm(id ItemID, ctx *tui.Context) bool {
	it := findItem(m.items, id)
	if it == nil || !it.selectable() {
		return false
	}
	m.setSelected(id)
	m.pressed, m.armed = id, true
	if ctx != nil {
		ctx.CapturePointer()
		m.captureCtx = ctx
		ctx.MarkDirty()
	}
	return true
}

// selectRow moves the selection, and re-arms ONLY when the row is the one the
// gesture started on. Dragging across a menu highlights rows; it does not change
// what a release would activate.
func (m *Menu) selectRow(id ItemID) bool {
	if id == "" {
		return false
	}
	it := findItem(m.items, id)
	if it == nil || !it.selectable() {
		return false
	}
	m.setSelected(id)
	if m.pressed != "" {
		m.armed = id == m.pressed
	}
	return true
}

// disarm clears the armed flag and RETAINS the pointer, so moving back over the
// pressed row re-arms it. Visual only.
func (m *Menu) disarm() bool {
	if !m.armed {
		return false
	}
	m.armed = false
	if ctx := m.Context(); ctx != nil {
		ctx.MarkDirty()
	}
	return true
}

// cancelGesture ends the pointer gesture and closes NOTHING.
//
// Idempotent, and true only when there was a gesture to end — so the runtime's
// cleanup paths can call it unconditionally, and a caller can tell "ended a
// drag" from "there was nothing to end".
func (m *Menu) cancelGesture() bool {
	if m.pressed == "" && !m.armed {
		return false
	}
	m.pressed, m.armed = "", false
	m.releaseCapture()
	if ctx := m.Context(); ctx != nil {
		ctx.MarkDirty()
	}
	return true
}

// releaseCapture ends the gesture's capture through the node that took it, and
// forgets it. Only the owner may release, so releasing through the Menu when a
// level took the pointer would silently do nothing.
func (m *Menu) releaseCapture() {
	if m.captureCtx != nil {
		m.captureCtx.ReleasePointer()
		m.captureCtx = nil
	}
}

// closeAction closes levels. Any active pointer gesture is cancelled FIRST, so a
// menu closed mid-drag does not leave the pointer captured by a widget that is
// no longer showing what the user was dragging over.
func (m *Menu) closeAction(all bool) bool {
	m.cancelGesture()
	if len(m.levels) == 0 {
		return all // Escape on a closed menu is still "handled"; Left is not
	}
	if all {
		m.Close()
		return true
	}
	parent := m.levels[len(m.levels)-1].parent
	m.closeLevelsFrom(len(m.levels) - 1)
	m.setSelected(parent) // back to the row that opened the level
	return true
}

// activate runs the kind table.
//
// STATE CHANGE, THEN ACTION, THEN CLOSE. An action observes the new state — a
// check handler that reads its own row sees the toggle that triggered it — and
// the menu closes only when the action was handled, because a command that could
// not run must not look like one that did.
func (m *Menu) activate(id ItemID, inv tui.ActionInvocation) bool {
	it := findItem(m.items, id)
	if it == nil || !it.selectable() {
		return false
	}
	// The gesture is over however this ends.
	m.pressed, m.armed = "", false
	m.releaseCapture()

	switch it.Kind {
	case ItemKindSeparator:
		return false // unreachable through selectable(), and stated anyway
	case ItemKindSubmenu:
		// No executor call and no activation event: opening a level is
		// navigation, not a command, and reporting it as one would make every
		// listener filter submenus back out.
		return m.openLevel(id, it.Children) == nil
	case ItemKindCheck:
		it.Checked = !it.Checked
	case ItemKindRadio:
		it.Checked = true
		clearGroupExcept(m.items, it.Group, it.ID)
	}

	handled := m.runRowAction(it, inv)
	if ctx := m.Context(); ctx != nil {
		ctx.Bus().Publish(MenuActivatedEvent{
			Owner: m.NodeID(), ItemID: id, Origin: inv.Origin, Handled: handled,
		})
		ctx.MarkDirty()
	}
	if handled {
		m.Close()
	}
	return true
}

// runRowAction hands the row's action to the executor, carrying the TRUSTED
// INCOMING INVOCATION with only the action replaced.
//
// Copying the invocation rather than building one is what preserves provenance
// end to end: the executor sees the Origin and Source the runtime recorded for
// the user's actual input, and the Menu has no way to state either, so it cannot
// claim a keypress produced something a click did.
func (m *Menu) runRowAction(it *MenuItemModel, inv tui.ActionInvocation) bool {
	// nilLike, not == nil: a row carrying a nil *myAction satisfies tui.Action
	// with a live type descriptor, and handing that to the executor makes a
	// consumer's own type switch reach a nil receiver for a row the model says
	// does nothing. The runtime defines a typed nil as no action; so does this.
	if nilLike(it.Action) || m.exec == nil {
		return false
	}
	forwarded := inv
	forwarded.Action = it.Action
	return m.exec(forwarded)
}

// HandleEvent cleans up the pointer gesture when the runtime takes the capture
// away — the owner was hidden, unmounted, or focus left its scope.
//
// The cleanup is exactly MenuCancelAction's: clear the gesture, activate
// nothing, and leave the open levels alone. A capture loss is not a decision the
// user made about the menu.
func (m *Menu) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.PointerCaptureLostEvent); ok {
		m.pressed, m.armed = "", false
		m.captureCtx = nil // the runtime has already taken it away
		if ctx := m.Context(); ctx != nil {
			ctx.MarkDirty()
		}
	}
	return false
}

// rowStateOf is the state one row is currently in. One function, so the Menu's
// own painter and a consumer's RowRenderer are answering from the same source
// rather than each deciding for itself.
//
// The four flags are INDEPENDENT and all four are computed: a row can be
// selected and open, or armed in a menu that has lost focus, and a renderer that
// cannot see the combination cannot draw it.
func (m *Menu) rowStateOf(it MenuItemModel) RowState {
	return RowState{
		Selected: it.ID == m.selected,
		Armed:    m.armed && it.ID == m.pressed,
		Focused:  m.hasFocus(),
		Open:     m.levelOpenFor(it.ID),
	}
}

// hasFocus reports whether the owning Menu contains focus, which is what a
// renderer needs in order to draw the selection differently when the menu is not
// the active widget. FocusWithin, not Focused: while a level is open the focus
// may legitimately be on a descendant.
func (m *Menu) hasFocus() bool {
	ctx := m.Context()
	if ctx == nil {
		return false
	}
	return ctx.Focused() || ctx.FocusWithin(m)
}

// levelOpenFor reports whether an open level hangs off this row.
func (m *Menu) levelOpenFor(id ItemID) bool {
	for _, lv := range m.levels {
		if lv.parent == id {
			return true
		}
	}
	return false
}

// rowWidth is the width one row wants: A PAD ON EACH SIDE, the label, a gap,
// the accelerator, and room for the mark or the submenu marker.
//
// THE PADS ARE PART OF THE ROW, not decoration the frame supplies. paintRow
// starts the label at x+1 and puts the submenu arrow at x+w-2, so a row sized to
// its bare content is one short at each end: the widest row of a level sets the
// popup's width, and it then painted its last cell onto the border. A submenu
// row lost a character of its label too, the arrow landing on top of it.
//
// It stayed hidden because only the WIDEST row of a level is affected, and a
// level whose widest row is a long plain command has slack from its neighbours
// to absorb the error. MenuItem.Layout — the standalone row — has always used
// measure(label)+2; this is the model-driven painter agreeing with it.
func (m *Menu) rowWidth(it MenuItemModel, marker bool) int {
	w := m.measure(it.Label) + 2
	if it.Accel != "" {
		w += 2 + m.measure(it.Accel)
	}
	if marker && it.Kind == ItemKindSubmenu {
		w += 2
	}
	if it.Kind == ItemKindCheck || it.Kind == ItemKindRadio {
		w += 2
	}
	return w
}

// paintRow paints one row, or delegates to the consumer's renderer.
//
// The delegation is total: a RowRenderer that is supplied paints the whole row,
// because a hook that painted only part of one would have to agree with this
// function about where the parts are, and the two would drift.
func (m *Menu) paintRow(s tui.Surface, it MenuItemModel, r tui.Rect, st RowState, marker bool) {
	base := rowStyle(m.style, viewOf(it), st)
	s.Fill(r, " ", base)
	// A plain != nil is correct HERE because the option normalised a typed nil
	// to absent before storing it. That is the whole point of normalising at the
	// boundary: one check at the door, and every reader afterwards can trust the
	// stored value instead of repeating it — a second check here would be a
	// second rule, and two rules are what come to disagree.
	if m.rows != nil {
		m.rows.RenderRow(s.Sub(r), viewOf(it), st)
		return
	}
	if it.Kind == ItemKindSeparator {
		for x := r.X; x < r.X+r.W; x++ {
			s.SetCell(x, r.Y, "─", m.style.Border())
		}
		return
	}
	x := r.X + 1
	if it.Kind == ItemKindCheck || it.Kind == ItemKindRadio {
		mark := " "
		if it.Checked {
			mark = "✓"
		}
		s.SetCell(x, r.Y, mark, base)
		x += 2
	}
	x = m.paintLabel(s, it, x, r.Y, base)
	if it.Accel != "" {
		ax := r.X + r.W - 1 - m.measure(it.Accel)
		if ax > x {
			m.paintText(s, it.Accel, ax, r.Y, m.style.Accel())
		}
	}
	if marker && it.Kind == ItemKindSubmenu {
		s.SetCell(r.X+r.W-2, r.Y, "▸", base)
	}
}

// paintLabel draws the label, underlining the mnemonic's grapheme cluster.
//
// Cluster-indexed rather than byte- or rune-indexed, because that is the unit a
// reader sees: an accented letter is one cluster and may be several runes, and
// underlining "the third rune" of such a label marks the wrong character.
func (m *Menu) paintLabel(s tui.Surface, it MenuItemModel, x, y int, base style.Style) int {
	i := 0
	for cluster := range tui.Graphemes(it.Label) {
		st := base
		if it.Hotkey != 0 && i == it.HotkeyIdx {
			st = base.Underline(true)
		}
		s.SetCell(x, y, cluster, st)
		x += s.StringWidth(cluster)
		i++
	}
	return x
}

// paintText draws plain text at a position, returning the next column.
func (m *Menu) paintText(s tui.Surface, text string, x, y int, st style.Style) int {
	for cluster := range tui.Graphemes(text) {
		s.SetCell(x, y, cluster, st)
		x += s.StringWidth(cluster)
	}
	return x
}
