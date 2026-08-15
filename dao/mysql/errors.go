package mysql

import (
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/yongjohnlee80/golib/dao"
)

// translateError maps a go-sql-driver *MySQLError number to a dao sentinel.
// MySQL reports the violated key only inside the human-readable message, so
// ConstraintError.Constraint is left empty (the sqlite precedent);
// errors.Is against the sentinels still works.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1062, 1586: // ER_DUP_ENTRY, ER_DUP_ENTRY_WITH_KEY_NAME
			return &dao.ConstraintError{Kind: dao.Unique, Err: dao.ErrDuplicate}
		case 1048: // ER_BAD_NULL_ERROR
			return &dao.ConstraintError{Kind: dao.NotNull, Err: dao.ErrNotNull}
		case 1216, 1217, 1451, 1452: // FK: no parent / row referenced (old + InnoDB pairs)
			return &dao.ConstraintError{Kind: dao.ForeignKey, Err: dao.ErrForeignKey}
		case 3819: // ER_CHECK_CONSTRAINT_VIOLATED
			return &dao.ConstraintError{Kind: dao.Check, Err: err}
		}
	}
	return err
}
