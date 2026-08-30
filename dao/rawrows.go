package dao

// Raw result access (ADR-0017 §2.3). [Rows] deliberately exposes only Scan:
// the engine's own read path always knows the Go types it is scanning into. A
// pass-through consumer does not — it forwards the target's own bytes and the
// server's own column metadata to its caller, and Scan would force a decode and
// re-encode through Go types it has no reason to name.
//
// Like [RowsColumns] (ADR-0012), this arrives as an optional extension of Rows
// rather than a wider Rows, so every existing implementation stays valid.

// FieldDescription is a result column's server-side descriptor, as reported by
// the driver. The field set mirrors the PostgreSQL RowDescription message,
// which is the only wire format that carries all of it.
type FieldDescription struct {
	// Name is the column's name in the result set.
	Name string
	// TableOID is the OID of the table the column came from, or 0 when the
	// column is not a simple table reference.
	TableOID uint32
	// ColumnAttr is the column's attribute number within that table, or 0.
	ColumnAttr uint16
	// TypeOID is the OID of the column's data type.
	TypeOID uint32
	// TypeSize is the type's size in bytes; negative for variable-length types.
	TypeSize int16
	// TypeModifier is the type-specific modifier (e.g. varchar length), or -1.
	TypeModifier int32
	// Format is the wire format of the values: 0 text, 1 binary. It tells a
	// pass-through consumer how to interpret [RawRows.RawValues].
	Format int16
}

// RawRows is an optional extension of [Rows]: a driver whose row stream can
// hand back the raw wire bytes and the server's own column descriptors
// implements it. Only postgres does — the pgx row stream already holds both,
// and dao would otherwise erase them.
//
// Probe it with [RawRowsOf]; absence is a capability miss, and the caller falls
// back to Scan.
type RawRows interface {
	Rows

	// RawValues returns the current row's column values as the driver received
	// them, in projection order, with the wire format reported by
	// [RawRows.Fields]. A NULL column is a nil slice; an empty non-NULL column
	// is a zero-length non-nil slice.
	//
	// The buffers are BORROWED: they are valid only until the next Next or
	// Close, after which the driver may overwrite them. A consumer that keeps a
	// value past that point must copy it — with bytes.Clone, not
	// append([]byte(nil), v...): appending zero bytes to a nil destination
	// yields nil, which would silently turn an empty value into a NULL one.
	RawValues() [][]byte

	// Fields returns the result set's column descriptors, in projection order.
	Fields() []FieldDescription
}

// RawRowsOf probes rows for the [RawRows] capability. Absence is reported as
// (nil, false) rather than an error: raw access is an optimization a consumer
// falls back from, not a failure — unlike [Columns], where the caller asked a
// question that has no answer without the capability.
func RawRowsOf(rows Rows) (RawRows, bool) {
	rr, ok := rows.(RawRows)
	return rr, ok
}
