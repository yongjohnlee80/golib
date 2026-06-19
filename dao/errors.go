package dao

import (
	"errors"
	"fmt"
)

// Package error sentinels. Driver errors are translated to these at the DAL
// boundary (see [Dialect.TranslateError]) so callers never see raw driver
// errors. Compare with errors.Is.
//
// This is the foundational set the driver layer (ADR-0004) needs; the error-map
// translation layer (ADR-0003 §5) extends it.
var (
	// ErrNoRows is returned when a single-row read finds nothing.
	ErrNoRows = errors.New("dao: no rows in result set")

	// ErrDuplicate is returned for a unique-constraint violation.
	ErrDuplicate = errors.New("dao: duplicate key violates unique constraint")

	// ErrNotNull is returned for a not-null-constraint violation.
	ErrNotNull = errors.New("dao: null value violates not-null constraint")

	// ErrForeignKey is returned for a foreign-key-constraint violation.
	ErrForeignKey = errors.New("dao: foreign key constraint violation")

	// ErrReadOnlyField is returned when a write stages a field marked ReadOnly
	// (computed/joined columns that must never appear in an INSERT/UPDATE).
	ErrReadOnlyField = errors.New("dao: cannot write a read-only field")

	// ErrNoConditions is returned by Update/Delete when no predicate is set, to
	// guard against an accidental full-table mutation.
	ErrNoConditions = errors.New("dao: no conditions set (refusing a full-table update/delete)")

	// ErrNothingToInsert is returned by Insert/Upsert when no values are staged.
	ErrNothingToInsert = errors.New("dao: no values to insert")

	// ErrUnknownField is returned (staged) when an intent method references a
	// field key that the schema does not declare.
	ErrUnknownField = errors.New("dao: unknown field")

	// ErrTransactionClosed is returned when a statement runs on a transaction that
	// has already committed or rolled back.
	ErrTransactionClosed = errors.New("dao: transaction already closed")

	// ErrUnknownConnection is returned when a tx-bound DAO's connection name is not
	// among the transaction's connections.
	ErrUnknownConnection = errors.New("dao: connection not in transaction")

	// ErrTwoPhaseUnsupported is returned by Commit when TwoPhase is requested but a
	// participating dialect does not support prepared transactions.
	ErrTwoPhaseUnsupported = errors.New("dao: dialect does not support two-phase commit")
)

// ConstraintKind classifies a constraint violation.
type ConstraintKind int

const (
	// UnknownConstraint is the zero value: an unclassified constraint.
	UnknownConstraint ConstraintKind = iota
	// Unique is a unique/primary-key constraint.
	Unique
	// NotNull is a not-null constraint.
	NotNull
	// ForeignKey is a foreign-key constraint.
	ForeignKey
	// Check is a check constraint.
	Check
)

// String returns the constraint kind's name.
func (k ConstraintKind) String() string {
	switch k {
	case Unique:
		return "unique"
	case NotNull:
		return "not-null"
	case ForeignKey:
		return "foreign-key"
	case Check:
		return "check"
	default:
		return "unknown"
	}
}

// ConstraintError carries the constraint name and kind for a constraint
// violation. It wraps a sentinel (e.g. [ErrDuplicate]) so errors.Is matches both
// the sentinel and, via As, the structured error.
type ConstraintError struct {
	// Constraint is the database's constraint name, when the driver reports it.
	Constraint string
	// Kind classifies the violation.
	Kind ConstraintKind
	// Err is the wrapped sentinel.
	Err error
}

// Error implements error.
func (e *ConstraintError) Error() string {
	if e.Constraint == "" {
		return fmt.Sprintf("dao: %s constraint violated: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("dao: %s constraint %q violated: %v", e.Kind, e.Constraint, e.Err)
}

// Unwrap returns the wrapped sentinel so errors.Is(err, ErrDuplicate) works.
func (e *ConstraintError) Unwrap() error { return e.Err }
