package widget

import (
	"errors"
	"fmt"
	"slices"

	"github.com/yongjohnlee80/golib/tui"
)

// THE MENU MODEL IS DATA, NOT COMPONENTS.
//
// A menu's rows are values a Menu owns and paints, not mounted children. That is
// the decision everything else follows from: a hundred-row menu costs a hundred
// struct values rather than a hundred nodes, a submenu three levels down is
// described before anything is on screen, and replacing the whole model is one
// assignment rather than a tree reconcile.
//
// It also costs something, honestly: a row is not a node, so it has no NodeID,
// cannot be hit-tested by the runtime, and cannot be armed by the generic
// recogniser. Menu therefore runs its own pointer state machine over the regions
// it declared, and that is the price of the model being data.

// ItemID is a caller-supplied row identity, stable across model replacement.
// Every mutation, selection and submenu operation names a row by this rather
// than by position, so inserting a row above does not silently retarget them.
type ItemID string

// ItemKind is what a row IS. The set is deliberately CLOSED.
//
// An earlier draft advertised that a new kind needed no switch edit, which is
// false for a closed enum with fixed fields: rendering and behaviour for an
// unknown kind require either a switch edit or a polymorphic row seam. Rather
// than promise openness the design does not have, the set is declared closed and
// extension goes through Action (behaviour) and RowRenderer (appearance) — which
// together cover the cases a new kind would have, without a switch edit and
// without every consumer inheriting a half-supported variant.
type ItemKind uint8

const (
	// KindCommand runs its Action and closes the menu. The zero value, so a
	// zero MenuItemModel is an inert command rather than something surprising.
	KindCommand ItemKind = iota
	// KindSubmenu opens a nested level; it has children and no action.
	KindSubmenu
	// KindSeparator is a visual rule. It cannot be selected or activated.
	KindSeparator
	// KindCheck toggles its own Checked state, then runs its Action.
	KindCheck
	// KindRadio sets itself and clears every other member of its Group, then
	// runs its Action.
	KindRadio
)

// String names the kind for traces and test failures.
func (k ItemKind) String() string {
	switch k {
	case KindCommand:
		return "command"
	case KindSubmenu:
		return "submenu"
	case KindSeparator:
		return "separator"
	case KindCheck:
		return "check"
	case KindRadio:
		return "radio"
	}
	return "unknown"
}

// Valid reports whether k is one of the declared kinds.
func (k ItemKind) Valid() bool { return k <= KindRadio }

// MenuItemModel is one row.
//
// ZERO VALUE: a command, disabled, invisible, inert. It shows nothing and does
// nothing, rather than being an enabled no-op that silently appears in a menu
// because a caller forgot a field. The constructors below set Enabled and
// Visible; anything else is stated explicitly on the value they return.
type MenuItemModel struct {
	// ID is this row's stable identity. Required, and unique across the whole
	// model including every level of children.
	ID ItemID
	// Kind is what the row is.
	Kind ItemKind
	// Label is the row's text.
	Label string
	// Hotkey is the mnemonic character, or 0 for none.
	Hotkey rune
	// HotkeyIdx is which GRAPHEME CLUSTER of Label to underline, 0-based. A
	// cluster index rather than a byte or rune offset, because that is the unit
	// a reader sees and the unit the renderer measures in.
	HotkeyIdx int
	// Accel is the accelerator text shown at the row's right edge, such as
	// "Ctrl+S". Display only: this widget never binds it.
	Accel string
	// Enabled rows can be selected and activated.
	Enabled bool
	// Visible rows are laid out and painted. An invisible row keeps its state —
	// visibility is presentation, checkedness is not.
	Visible bool
	// Checked applies to KindCheck and KindRadio.
	Checked bool
	// Group names a radio set. Activating one member clears the others.
	Group string
	// Action is what a Command, Check or Radio runs. nil is inert.
	//
	// tui-qualified deliberately: this package already exports an unrelated
	// widget.Action, and an unqualified name here would read as that one.
	Action tui.Action
	// Children are a submenu's rows.
	Children []MenuItemModel
}

// NewCommand builds an enabled, visible command row.
func NewCommand(id ItemID, label string, action tui.Action) MenuItemModel {
	return MenuItemModel{ID: id, Kind: KindCommand, Label: label,
		Enabled: true, Visible: true, Action: action}
}

// NewSubmenu builds an enabled, visible submenu row over a deep copy of
// children, so the caller's slice and the returned value cannot alias.
func NewSubmenu(id ItemID, label string, children []MenuItemModel) MenuItemModel {
	return MenuItemModel{ID: id, Kind: KindSubmenu, Label: label,
		Enabled: true, Visible: true, Children: copyItems(children)}
}

// NewSeparator builds a visible separator. It is deliberately NOT enabled:
// a separator is never selectable, and marking it enabled would invite code to
// treat "enabled" as the only selectability test.
func NewSeparator(id ItemID) MenuItemModel {
	return MenuItemModel{ID: id, Kind: KindSeparator, Visible: true}
}

// NewCheck builds an enabled, visible, UNCHECKED check row. A caller wanting it
// checked sets Checked on the returned value.
func NewCheck(id ItemID, label string, action tui.Action) MenuItemModel {
	return MenuItemModel{ID: id, Kind: KindCheck, Label: label,
		Enabled: true, Visible: true, Action: action}
}

// NewRadio builds an enabled, visible, unchecked radio row in group.
func NewRadio(id ItemID, label, group string, action tui.Action) MenuItemModel {
	return MenuItemModel{ID: id, Kind: KindRadio, Label: label, Group: group,
		Enabled: true, Visible: true, Action: action}
}

// selectable reports whether a row can hold the selection or be activated.
//
// Three conditions, and all three matter: a separator is structurally
// unselectable whatever its flags say, a disabled row is temporarily so, and an
// invisible row is not on screen to select. Anywhere that checks only Enabled
// eventually lands the selection on a separator.
func (m MenuItemModel) selectable() bool {
	return m.Visible && m.Enabled && m.Kind != KindSeparator
}

// copyItems deep-copies a model slice, so a caller mutating its own data cannot
// reach inside a mounted Menu and a Menu cannot hand its own storage out.
//
// The recursion is the point: a shallow copy shares the Children slices, which
// is precisely the aliasing this exists to prevent.
func copyItems(items []MenuItemModel) []MenuItemModel {
	if len(items) == 0 {
		return nil
	}
	out := make([]MenuItemModel, len(items))
	for i, it := range items {
		it.Children = copyItems(it.Children)
		out[i] = it
	}
	return out
}

// ErrInvalidMenuModel is the umbrella every model rejection matches.
var ErrInvalidMenuModel = errors.New("widget: invalid menu model")

// ErrEmptyItemID reports a row with no ID.
var ErrEmptyItemID = errors.New("widget: empty ItemID")

// ErrDuplicateItemID reports the same ItemID twice anywhere in the model.
var ErrDuplicateItemID = errors.New("widget: duplicate ItemID")

// ErrUnknownItemKind reports a row carrying a value outside the closed set.
var ErrUnknownItemKind = errors.New("widget: unknown ItemKind")

// menuModelError carries both identities, like the button-list error: a caller
// may handle "the model was bad" or the specific fault without the two
// competing for a single-error chain. Unexported, because the published contract
// is the sentinels and errors.Is.
type menuModelError struct {
	kind   error
	detail string
}

func (e *menuModelError) Error() string { return e.kind.Error() + ": " + e.detail }

// Unwrap returns both identities; errors.Is walks every branch.
func (e *menuModelError) Unwrap() []error { return []error{ErrInvalidMenuModel, e.kind} }

// validateItems checks a whole model before any of it is applied: every ID
// non-empty, every ID unique ACROSS ALL LEVELS, every kind declared.
//
// Recursive uniqueness rather than per-level, because every operation this
// widget offers names a row by ID alone — SetChecked, Select, Open. With the
// same ID at two depths those become ambiguous, and an ambiguous mutation is
// worse than a rejected model: it silently picks one.
//
// Pure, and reports the first fault it finds, so the caller decides what to do
// before anything changes.
func validateItems(items []MenuItemModel, seen map[ItemID]bool, path string) error {
	for i, it := range items {
		where := fmt.Sprintf("%s[%d]", path, i)
		if it.ID == "" {
			return &menuModelError{kind: ErrEmptyItemID,
				detail: where + " has no ID; every row is addressed by ID, so a row without one cannot be selected, checked or opened"}
		}
		if !it.Kind.Valid() {
			return &menuModelError{kind: ErrUnknownItemKind,
				detail: fmt.Sprintf("%s (%q) has kind %d, outside the closed set", where, it.ID, it.Kind)}
		}
		if seen[it.ID] {
			return &menuModelError{kind: ErrDuplicateItemID,
				detail: fmt.Sprintf("%q appears more than once, at %s; IDs are unique across every level, or ID-based selection and mutation cannot be deterministic", it.ID, where)}
		}
		seen[it.ID] = true
		if err := validateItems(it.Children, seen, where+".Children"); err != nil {
			return err
		}
	}
	return nil
}

// findItem walks the model for id and returns a pointer into the live storage,
// so a caller can mutate the row in place. nil when there is no such row.
//
// A pointer rather than a value, because every mutator here changes one field of
// one row and copying it back by index through an unknown depth is the part that
// goes wrong.
func findItem(items []MenuItemModel, id ItemID) *MenuItemModel {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
		if found := findItem(items[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

// clearGroupExcept unchecks every radio row in group other than keep, at every
// depth. Radio exclusivity is a property of the whole model rather than of one
// level: a group split across a submenu is unusual but expressible, and honoring
// it only within a level would leave two members checked.
func clearGroupExcept(items []MenuItemModel, group string, keep ItemID) {
	for i := range items {
		it := &items[i]
		if it.Kind == KindRadio && it.Group == group && it.ID != keep {
			it.Checked = false
		}
		clearGroupExcept(it.Children, group, keep)
	}
}

// RowView is an IMMUTABLE projection of one row, handed to a RowRenderer.
//
// It deliberately omits Children. Passing the model value itself would hand out
// a slice header that aliases the Menu's own storage, so a renderer — which is
// consumer code running inside the render phase — could mutate the model the
// Menu owns. HasChildren carries the only fact a renderer needs from it.
type RowView struct {
	ID          ItemID
	Kind        ItemKind
	Label       string
	Hotkey      rune
	HotkeyIdx   int
	Accel       string
	Enabled     bool
	Checked     bool
	HasChildren bool
}

// viewOf projects a row for rendering.
func viewOf(m MenuItemModel) RowView {
	return RowView{
		ID: m.ID, Kind: m.Kind, Label: m.Label, Hotkey: m.Hotkey,
		HotkeyIdx: m.HotkeyIdx, Accel: m.Accel, Enabled: m.Enabled,
		Checked: m.Checked, HasChildren: len(m.Children) > 0,
	}
}

// RowState is how a row should currently look.
type RowState uint8

const (
	// RowStateNormal: an ordinary enabled row.
	RowStateNormal RowState = iota
	// RowStateSelected: the row the keyboard would act on.
	RowStateSelected
	// RowStateArmed: pressed and not yet released.
	RowStateArmed
	// RowStateDisabled: present but not interactive.
	RowStateDisabled
)

// String names the state for traces and test failures.
func (r RowState) String() string {
	switch r {
	case RowStateNormal:
		return "normal"
	case RowStateSelected:
		return "selected"
	case RowStateArmed:
		return "armed"
	case RowStateDisabled:
		return "disabled"
	}
	return "unknown"
}

// RowRenderer paints one row into the rect the Menu laid out for it.
//
// It is the declared extension seam for kinds the closed ItemKind does not
// provide: a consumer wanting a colour swatch row supplies the appearance here
// and the behaviour through Action, without a new kind and without a switch edit
// anywhere in this package.
//
// PURE. No state mutation, no MarkDirty, no layout — the render-phase rules
// apply to a hook exactly as they apply to a widget. It may not change the row's
// geometry either: the Menu owns sizing, so that rows stay uniform and the
// regions it declared for hit-testing keep matching what is on screen.
type RowRenderer interface {
	RenderRow(s tui.Surface, row RowView, st RowState)
}

// visibleRows returns the indices of the rows a level actually shows, in order.
// Separators are included — they are painted — but are not selectable.
func visibleRows(items []MenuItemModel) []int {
	out := make([]int, 0, len(items))
	for i := range items {
		if items[i].Visible {
			out = append(out, i)
		}
	}
	return slices.Clip(out)
}
