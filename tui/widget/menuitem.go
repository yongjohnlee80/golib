package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// MenuItem is a STANDALONE menu-shaped control: a mountable, focusable leaf for
// ordinary layouts — a command palette row, a sidebar entry, a context action
// rendered inline.
//
// TWO TYPES, ONE CONTRACT, and the distinction is worth stating because the
// names are close. MenuItemModel is inert DATA owned by a Menu; rendering one
// does not mount anything. MenuItem is a COMPONENT with its own node, so it is
// hit-tested, armed and activated by the runtime's generic recogniser exactly
// like a Button — and it emits ControlActivatedEvent, because Owner identifies
// it, where a model row needs MenuActivatedEvent to say which row it was.
//
// Both resolve activation through the same runtime path, so a MenuItem and a
// Menu row triggered by the same input behave the same way.
type MenuItem struct {
	Base

	label   string
	accel   string
	checked bool
	kind    ItemKind
	enabled bool
	onRun   func()
	st      *MenuStyle

	armed         bool
	pointerPolicy tui.PointerPolicy
}

// MenuItemOption configures a MenuItem at construction.
type MenuItemOption func(*MenuItem)

// NewMenuItem builds a standalone, enabled menu entry.
func NewMenuItem(label string, opts ...MenuItemOption) *MenuItem {
	m := &MenuItem{
		label:         label,
		kind:          KindCommand,
		enabled:       true,
		pointerPolicy: tui.PointerInherit,
	}
	for _, o := range opts {
		if o != nil {
			o(m)
		}
	}
	if !m.kind.Valid() || m.kind == KindSubmenu || m.kind == KindSeparator {
		panic(tuiFatal("widget: NewMenuItem",
			"a standalone item is a command, a check or a radio; a submenu needs a Menu and a separator is not a control",
			int(m.kind)))
	}
	return m
}

// WithItemKind sets whether the item is a command, a check or a radio.
func WithItemKind(k ItemKind) MenuItemOption {
	return func(m *MenuItem) { m.kind = k }
}

// WithItemAccel sets the accelerator text shown at the right edge. Display only:
// this widget never binds it.
func WithItemAccel(s string) MenuItemOption {
	return func(m *MenuItem) { m.accel = s }
}

// WithItemChecked sets the initial checked state of a check or radio item.
func WithItemChecked(v bool) MenuItemOption {
	return func(m *MenuItem) { m.checked = v }
}

// WithItemEnabled sets the initial availability.
func WithItemEnabled(v bool) MenuItemOption {
	return func(m *MenuItem) { m.enabled = v }
}

// WithOnRun registers the callback run on activation.
func WithOnRun(fn func()) MenuItemOption {
	return func(m *MenuItem) { m.onRun = fn }
}

// WithItemStyle associates a style. Items share MenuStyle with Menu, so one
// value dresses both and an inline item matches the menu it belongs with.
func WithItemStyle(s *MenuStyle) MenuItemOption {
	return func(m *MenuItem) { m.st = s }
}

// Label reports the item's text.
func (m *MenuItem) Label() string { return m.label }

// Kind reports what the item is.
func (m *MenuItem) Kind() ItemKind { return m.kind }

// Checked reports the check or radio state.
func (m *MenuItem) Checked() bool { return m.checked }

// SetChecked sets the state and repaints.
func (m *MenuItem) SetChecked(v bool) {
	if m.checked == v {
		return
	}
	m.checked = v
	m.MarkDirty()
}

// Enabled reports whether the item can be activated.
func (m *MenuItem) Enabled() bool { return m.enabled }

// SetEnabled sets availability, repaints, and tells the runtime that
// focusability across the scope has changed — a disabled item is not a tab stop,
// and the focus currently resting on it has to move.
func (m *MenuItem) SetEnabled(v bool) {
	if m.enabled == v {
		return
	}
	m.enabled = v
	if ctx := m.Context(); ctx != nil {
		ctx.InvalidateFocusability()
		ctx.MarkDirty()
	}
}

// WithStyle associates a style at runtime and returns the item for chaining.
// nil reverts to the default look.
func (m *MenuItem) WithStyle(s *MenuStyle) *MenuItem {
	m.st = s
	m.MarkDirty()
	return m
}

// WithPointerPolicy sets whether this item accepts pointer input, and returns it
// for chaining. Remembered as well as applied, because
// NewMenuItem(...).WithPointerPolicy(...) runs before there is any Context.
func (m *MenuItem) WithPointerPolicy(p tui.PointerPolicy) *MenuItem {
	if !p.Valid() {
		panic(tuiFatal("widget: MenuItem.WithPointerPolicy",
			"value outside PointerInherit, PointerEnabled, PointerDisabled", int(p)))
	}
	m.pointerPolicy = p
	if ctx := m.Context(); ctx != nil {
		ctx.SetPointerPolicy(p)
	}
	return m
}

// AcceptsFocus reports that an enabled item is a tab stop.
func (m *MenuItem) AcceptsFocus() bool { return m.enabled }

// ActivationAvailable tells the runtime not to start a gesture whose only
// outcome would be a refused activation.
func (m *MenuItem) ActivationAvailable() bool { return m.enabled }

// Activate runs the item: a check or radio toggles first, then the callback
// runs, so a handler reading its own state sees the change that triggered it.
//
// Returns false when disabled, which is what stops the runtime publishing an
// activation that did not happen.
func (m *MenuItem) Activate(tui.ActionOrigin) bool {
	if !m.enabled {
		return false
	}
	switch m.kind {
	case KindCheck:
		m.checked = !m.checked
	case KindRadio:
		// A standalone radio has no group to clear: exclusivity belongs to
		// whatever owns the set, and inventing a package-level registry of
		// loose radios would be shared mutable state nobody asked for.
		m.checked = true
	}
	if m.onRun != nil {
		m.onRun()
	}
	m.MarkDirty()
	return true
}

// SetArmed records the pressed look. Visual only, and driven solely by the
// runtime's recogniser.
func (m *MenuItem) SetArmed(v bool) {
	if m.armed == v {
		return
	}
	m.armed = v
	m.MarkDirty()
}

// Armed reports the pressed look, for tests and for a container that mirrors it.
func (m *MenuItem) Armed() bool { return m.armed }

// Init installs the conventional activation keys and applies a pointer policy
// requested before there was a Context to apply it to.
//
// The SAME resolver Button uses, deliberately, rather than a second one that
// happened to bind the same keys: a standalone item and a button are both leaf
// controls, and two resolvers agreeing today is two resolvers that can disagree
// tomorrow.
func (m *MenuItem) Init(ctx *tui.Context) {
	m.Base.Init(ctx)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(activateKeys))
	ctx.SetPointerPolicy(m.pointerPolicy)
}

// state is the item's current look, using the same vocabulary as a Menu row so
// an inline item and a menu row in the same state look the same.
func (m *MenuItem) state() RowState {
	switch {
	case !m.enabled:
		return RowStateDisabled
	case m.armed:
		return RowStateArmed
	case m.focused():
		return RowStateSelected
	}
	return RowStateNormal
}

// Layout sizes the item to its content: a mark column for a check or radio, the
// label, a gap, and the accelerator.
func (m *MenuItem) Layout(cs tui.Constraints) tui.Size {
	w := m.measure(m.label) + 2
	if m.kind == KindCheck || m.kind == KindRadio {
		w += 2
	}
	if m.accel != "" {
		w += 2 + m.measure(m.accel)
	}
	return cs.Constrain(tui.Size{W: w, H: 1})
}

// Render paints the item.
func (m *MenuItem) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	st := m.st.Row(m.state())
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, " ", st)

	x := 1
	if m.kind == KindCheck || m.kind == KindRadio {
		mark := " "
		if m.checked {
			mark = "✓"
		}
		s.SetCell(x, 0, mark, st)
		x += 2
	}
	for cluster := range tui.Graphemes(m.label) {
		if x >= sz.W {
			break
		}
		s.SetCell(x, 0, cluster, st)
		x += s.StringWidth(cluster)
	}
	if m.accel == "" {
		return
	}
	ax := sz.W - 1 - m.measure(m.accel)
	if ax <= x {
		return // no room without overwriting the label
	}
	for cluster := range tui.Graphemes(m.accel) {
		s.SetCell(ax, 0, cluster, m.st.Accel())
		ax += s.StringWidth(cluster)
	}
}
