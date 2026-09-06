package postgres

import (
	"github.com/yongjohnlee80/golib/dao"
)

// Raw result access for the Postgres driver. pgx already holds
// the current row's wire bytes and the server's RowDescription; dao.Rows erased
// both, forcing a pass-through consumer to decode into Go types it has no
// reason to name and re-encode them. pgxRows opts into dao.RawRows to hand them
// straight through.

// RawValues returns the current row's column values exactly as pgx received
// them, satisfying dao.RawRows.
//
// The slices are pgx's own receive buffers, not copies: they are valid only
// until the next Next or Close, after which pgx may reuse the memory. A
// consumer that keeps a value beyond the current row must copy it. Returning a
// copy here instead would defeat the whole point of the capability — the
// pass-through path exists to avoid exactly that allocation.
func (r *pgxRows) RawValues() [][]byte { return r.rows.RawValues() }

// Fields returns the result set's column descriptors as the server sent them,
// satisfying dao.RawRows. pgconn's field names are byte-for-byte the
// RowDescription fields; only the Go spellings differ.
func (r *pgxRows) Fields() []dao.FieldDescription {
	fds := r.rows.FieldDescriptions()
	out := make([]dao.FieldDescription, len(fds))
	for i, fd := range fds {
		out[i] = dao.FieldDescription{
			Name:         fd.Name,
			TableOID:     fd.TableOID,
			ColumnAttr:   fd.TableAttributeNumber,
			TypeOID:      fd.DataTypeOID,
			TypeSize:     fd.DataTypeSize,
			TypeModifier: fd.TypeModifier,
			Format:       fd.Format,
		}
	}
	return out
}
