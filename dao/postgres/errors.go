package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yongjohnlee80/golib/dao"
)

// translateError converts a pgx/pgconn driver error into a dao sentinel. For the
// constraint-violation SQLSTATEs it returns a *dao.ConstraintError carrying the
// constraint name (so a per-entity dao.Errors map can match it). Unrecognized
// errors are returned unchanged.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505": // unique_violation
			return &dao.ConstraintError{Constraint: pg.ConstraintName, Kind: dao.Unique, Err: dao.ErrDuplicate}
		case "23502": // not_null_violation
			return &dao.ConstraintError{Constraint: notNullName(pg), Kind: dao.NotNull, Err: dao.ErrNotNull}
		case "23503": // foreign_key_violation
			return &dao.ConstraintError{Constraint: pg.ConstraintName, Kind: dao.ForeignKey, Err: dao.ErrForeignKey}
		}
	}
	return err
}

// notNullName returns the best identifier a not-null violation carries: Postgres
// sets ColumnName (not ConstraintName) for 23502.
func notNullName(pg *pgconn.PgError) string {
	if pg.ConstraintName != "" {
		return pg.ConstraintName
	}
	return pg.ColumnName
}
