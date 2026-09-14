package widget

import "errors"

// ROLE VALIDATION, ONCE.
//
// The same rule — at most one Default and one Cancel among the buttons of one
// container — is needed in two places with incompatible failure modes.
// Construction options cannot return an error, so a bad button list there is a
// panic; a runtime setter must return one, because rejecting a caller's input
// at runtime is an ordinary outcome rather than a programmer error.
//
// Written twice, those two drift: one gains a rule the other lacks, and a
// dialog then accepts through SetButtons a list its own constructor would have
// refused. So the rule is a PURE function here, and the panic and the error are
// two thin adapters over it that cannot disagree.

// ErrDuplicateButtonRole reports a button list carrying two Defaults or two
// Cancels. Callers match it with errors.Is.
var ErrDuplicateButtonRole = errors.New("widget: duplicate button role")

// roleFault describes a failed validation precisely enough for both adapters:
// the error text and the panic detail are built from the same words.
type roleFault struct {
	role  ButtonRole // the role seen twice
	first int        // index of the first button carrying it
	dup   int        // index of the second
}

// describe renders the fault for a human, naming both offenders. An error that
// says only "duplicate role" leaves the caller counting buttons by hand.
func (f roleFault) describe() string {
	return "at most one Button with role " + f.role.String() +
		" per container; index " + itoa(f.first) + " and index " + itoa(f.dup) +
		" both carry it"
}

// validateButtonRoles is the ONE rule. It reports the first fault it finds, or
// nil when the list is acceptable.
//
// Pure: it inspects and returns, mutating nothing and panicking never, so both
// adapters can call it before deciding what to do about the answer — which is
// what makes "validate before any mutation" achievable rather than aspirational.
//
// Nil entries are ignored rather than rejected here: whether a nil button is
// acceptable is the caller's question, and each adapter answers it separately.
// Unknown roles are ignored too, so an application-defined role stays additive.
func validateButtonRoles(buttons []*Button) *roleFault {
	seen := map[ButtonRole]int{}
	for i, b := range buttons {
		if b == nil {
			continue
		}
		switch b.role {
		case ButtonRoleDefault, ButtonRoleCancel:
			if first, dup := seen[b.role]; dup {
				return &roleFault{role: b.role, first: first, dup: i}
			}
			seen[b.role] = i
		}
	}
	return nil
}
