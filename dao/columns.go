package dao

import "fmt"

// Result-set column names: the engine's own scan path never needs
// them (projection and scan plan come from the same Schema.resolve pass), but
// consumers running raw SQL through [DataConn]'s Querier surface do. Drivers
// expose the capability through the optional [RowsColumns] extension rather
// than a wider [Rows] interface, so existing Rows implementations stay valid.

// RowsColumns is an optional extension of [Rows]: a driver whose row stream
// can report the result set's column names implements it. The stdlib
// *sql.Rows satisfies it natively, so database/sql-backed drivers (sqlite,
// mysql) get it for free; other drivers adapt their native metadata (pgx maps
// FieldDescriptions).
type RowsColumns interface {
	Rows

	// Columns returns the result set's column names, in projection order.
	Columns() ([]string, error)
}

// Columns reports the column names of rows when its driver exposes them. It
// returns ErrUnsupported (wrapped) when rows does not implement
// [RowsColumns] — never a silent empty slice.
func Columns(rows Rows) ([]string, error) {
	if rc, ok := rows.(RowsColumns); ok {
		return rc.Columns()
	}
	return nil, fmt.Errorf("%w: result column names", ErrUnsupported)
}
