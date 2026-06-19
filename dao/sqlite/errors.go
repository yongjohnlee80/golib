package sqlite

import (
	"errors"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/yongjohnlee80/golib/dao"
)

// translateError maps a modernc SQLite extended result code to a dao sentinel.
// SQLite does not report a structured constraint name, so ConstraintError.Constraint
// is left empty; errors.Is against the sentinels still works.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return &dao.ConstraintError{Kind: dao.Unique, Err: dao.ErrDuplicate}
		case sqlite3.SQLITE_CONSTRAINT_NOTNULL:
			return &dao.ConstraintError{Kind: dao.NotNull, Err: dao.ErrNotNull}
		case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
			return &dao.ConstraintError{Kind: dao.ForeignKey, Err: dao.ErrForeignKey}
		}
	}
	return err
}
