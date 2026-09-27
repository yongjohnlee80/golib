package dao

import "fmt"

// AffectedUpdater is an optional DAO capability: Update, reporting how many
// rows it changed.
//
// It exists for the compare-and-set: an UPDATE conditioned on a row's current
// state ("... WHERE id = ? AND state = 'open'") only means something if the
// caller can tell whether the condition held, and Update alone cannot say —
// it returns nil for zero rows as for one. A caller that needs to know writes
// its condition as predicates and reads the count:
//
//	n, err := dao.UpdateAffected(schema.On(tx).
//		With(ID, id).With(State, "open").Set(State, "closed"))
//	// n == 0: the row was not open — someone else closed it first
//
// The count is the driver's. PostgreSQL and SQLite count the rows the WHERE
// matched; MySQL counts the rows it CHANGED unless the DSN asks for found rows
// (clientFoundRows=true), so on MySQL an update that sets a row to the values it
// already has reports 0. A compare-and-set whose Set always changes the row —
// the condition's column moving to a new value — means the same on all three.
// A driver that cannot report a count gets [ErrUnsupported] rather than a
// guess, because the one quiet answer — zero — would read as "the condition
// did not hold" when nothing is known.
type AffectedUpdater interface {
	UpdateAffected() (int64, error)
}

// UpdateAffected runs d's Update and returns the rows it changed. It returns
// [ErrUnsupported] when d does not implement [AffectedUpdater] or its driver
// did not report a count, and Update's own errors otherwise
// ([ErrNoConditions] with no predicate). An empty value set is a no-op, as for
// Update, and reports 0.
func UpdateAffected[R any, C ~string, ID any](d DAO[R, C, ID]) (int64, error) {
	u, ok := d.(AffectedUpdater)
	if !ok {
		return 0, fmt.Errorf("%w: rows affected (%T does not report them)", ErrUnsupported, d)
	}
	return u.UpdateAffected()
}
