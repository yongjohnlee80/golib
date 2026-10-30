package dao

import (
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// RowLock is a locking read's strength, for [WithRowLock].
type RowLock int

// The strengths [WithRowLock] takes.
const (
	// LockForUpdate locks the rows read against every other lock and write until the
	// transaction ends: FOR UPDATE.
	LockForUpdate RowLock = iota + 1
	// LockForShare locks them against writes and FOR UPDATE, not against other FOR SHARE
	// readers: FOR SHARE.
	LockForShare
)

// String names the strength.
func (l RowLock) String() string {
	switch l {
	case LockForUpdate:
		return "FOR UPDATE"
	case LockForShare:
		return "FOR SHARE"
	}
	return fmt.Sprintf("RowLock(%d)", int(l))
}

// RowLocker is an optional [Dialect] capability: locking reads. [WithRowLock] needs it.
type RowLocker interface {
	// RowLockClause is the clause appended to a SELECT that locks the rows it reads of table
	// (the schema's table, as declared, possibly schema-qualified) and of no other.
	RowLockClause(l RowLock, table string) string
}

// ErrRowLockNeedsTx is a verb's answer for a DAO acquired with [WithRowLock] that is not on a
// transaction: a pool statement's locks end with that statement, so the verb refuses before
// sending anything instead of locking nothing. It wraps errs.ErrPrecondition.
var ErrRowLockNeedsTx = fmt.Errorf("dao: WithRowLock needs the DAO on a transaction (%w)", errs.ErrPrecondition)

// WithRowLock makes the DAO's row reads, Get, Select and Iterate, lock the rows they read of
// the schema's own table (never a joined table's) until the transaction ends.
//
// The DAO must be on a transaction (On(tx), OnCtx with a transaction, or Use(tx)); without one
// every verb answers [ErrRowLockNeedsTx]. The dialect must implement [RowLocker], or the DAO
// answers [ErrUnsupported]; a strength outside the constants fails the DAO with an error
// wrapping errs.ErrInvalidArgument. What a lock can't be put on answers ErrUnsupported:
// Exists, Count and CountDistinct (aggregates), SelectDistinct, the writes (which lock their
// rows already) and Batch. Every refusal comes before anything is sent.
func WithRowLock(l RowLock) QueryOption {
	return func(c *queryConfig) { c.rowLock, c.rowLockSet = l, true }
}

// checkRowLock is the acquisition-time check of WithRowLock's strength against the dialect.
func checkRowLock(d Dialect, l RowLock, set bool) error {
	if !set {
		return nil
	}
	switch l {
	case LockForUpdate, LockForShare:
	default:
		return errs.Wrap(errs.ErrInvalidArgument, "dao: no row lock %s", l)
	}
	if _, ok := d.(RowLocker); !ok {
		return fmt.Errorf("%w: row locks (dialect %s)", ErrUnsupported, d.Name())
	}
	return nil
}

// lockable is a locked DAO's answer for a statement of op: refused unless it is a row read
// (not DISTINCT) on a transaction.
func (d *queryDAO[R, C, K, ID]) lockable(op Op) error {
	switch op {
	case OpGet, OpSelect, OpIterate:
		if d.q.distinct {
			return fmt.Errorf("%w: a row lock on SELECT DISTINCT", ErrUnsupported)
		}
	default:
		return fmt.Errorf("%w: a row lock on %s", ErrUnsupported, op)
	}
	if d.tx == nil {
		return ErrRowLockNeedsTx
	}
	return nil
}
