package widget

import (
	"errors"

	"github.com/yongjohnlee80/golib/tui"
)

// BUTTON-LIST VALIDATION, ONCE.
//
// The same rules are needed in two places with incompatible failure modes.
// Construction options cannot return an error, so a bad button list there is a
// panic; a runtime setter must return one, because rejecting a caller's input at
// runtime is an ordinary outcome rather than a programmer error.
//
// Written twice, those two drift: one gains a rule the other lacks, and a dialog
// then accepts through SetButtons a list its own constructor would have refused.
// So the rules are a PURE function here, and the panic and the error are two
// thin adapters over it that cannot disagree.
//
// It is a TRANSACTION validator: it answers for the WHOLE list before any of it
// is applied. That is what lets a rejected call leave the dialog exactly as it
// was. A validator that checked while installing would install the acceptable
// entries and then fail, which is the worst of both.

// ErrInvalidButtonList reports a button list this container cannot accept.
// Callers match it, and the more specific errors below, with errors.Is.
var ErrInvalidButtonList = errors.New("widget: invalid button list")

// ErrDuplicateButtonRole reports a list carrying two Defaults or two Cancels.
var ErrDuplicateButtonRole = errors.New("widget: duplicate button role")

// ErrNilButton reports a nil entry in a button list.
var ErrNilButton = errors.New("widget: nil button")

// ErrRepeatedButton reports the same *Button appearing twice in one list.
var ErrRepeatedButton = errors.New("widget: repeated button")

// ErrForeignButton reports a button already mounted somewhere else in the tree.
var ErrForeignButton = errors.New("widget: button mounted elsewhere")

// listFault describes a failed validation precisely enough for both adapters:
// the error text and the panic detail are built from the same words.
type listFault struct {
	kind  error      // one of the sentinels above
	role  ButtonRole // set for a duplicate role
	first int        // index of the first offender, or -1
	dup   int        // index of the second offender
}

// describe renders the fault for a human, naming the offenders by index. An
// error that says only "invalid list" leaves the caller counting buttons by hand.
func (f listFault) describe() string {
	switch f.kind {
	case ErrDuplicateButtonRole:
		return "at most one Button with role " + f.role.String() +
			" per container; index " + itoa(f.first) + " and index " + itoa(f.dup) +
			" both carry it"
	case ErrNilButton:
		return "index " + itoa(f.dup) + " is nil; a button list states the controls " +
			"a dialog has, and a hole in it is not one of them"
	case ErrRepeatedButton:
		return "the same *Button appears at index " + itoa(f.first) + " and index " +
			itoa(f.dup) + "; one component cannot be mounted at two places in the tree"
	case ErrForeignButton:
		return "index " + itoa(f.dup) + " is already mounted elsewhere in the tree; " +
			"adopting it would unmount it from its current parent behind that parent's back"
	}
	return "invalid button list"
}

// validateButtonList is the ONE rule set. It reports the first fault it finds,
// or nil when the list is acceptable.
//
// Pure: it inspects and returns, mutating nothing and panicking never, so both
// adapters can call it before deciding what to do about the answer — which is
// what makes "validate before any mutation" achievable rather than aspirational.
//
// owner is the card the buttons are being installed into, and may be nil before
// mount. It is needed only to tell "already mounted HERE" — an ordinary retained
// button in a reorder — from "already mounted SOMEWHERE ELSE", which is the
// error. Without the distinction, every reorder would be rejected.
//
// Unknown roles are ignored, so an application-defined role stays additive.
func validateButtonList(buttons []*Button, owner tui.Component) *listFault {
	seen := map[ButtonRole]int{}
	at := map[*Button]int{}
	for i, b := range buttons {
		if b == nil {
			return &listFault{kind: ErrNilButton, first: -1, dup: i}
		}
		if first, repeated := at[b]; repeated {
			return &listFault{kind: ErrRepeatedButton, first: first, dup: i}
		}
		at[b] = i
		// A button that is mounted, and whose parent is not the container about
		// to adopt it, belongs to another subtree. Mounting it here panics
		// inside the runtime AFTER the departures have already been unmounted,
		// which is precisely the half-applied state this validator exists to
		// make unreachable.
		if ctx := b.Context(); ctx != nil && !ctx.ParentIs(owner) {
			return &listFault{kind: ErrForeignButton, first: -1, dup: i}
		}
		switch b.role {
		case ButtonRoleDefault, ButtonRoleCancel:
			if first, dup := seen[b.role]; dup {
				return &listFault{kind: ErrDuplicateButtonRole, role: b.role, first: first, dup: i}
			}
			seen[b.role] = i
		}
	}
	return nil
}
