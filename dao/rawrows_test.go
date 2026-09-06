package dao

import (
	"bytes"
	"testing"
)

// The raw-result capability at the core level:
// the probe, and the borrowed-buffer contract.

// --- fakes -------------------------------------------------------------------

// reuseRows is a RawRows whose per-row buffers are REUSED, the way a driver
// that decodes into a scratch buffer behaves. It exists so the borrowed-buffer
// contract can be tested as a property of consumers (do they copy?) rather than
// as an assertion that some real driver happens to overwrite its memory today.
type reuseRows struct {
	rows [][][]byte // logical row values
	pos  int
	buf  [][]byte // the single set of buffers handed out, rewritten per row
	fds  []FieldDescription
}

func newReuseRows(fds []FieldDescription, rows ...[][]byte) *reuseRows {
	return &reuseRows{rows: rows, pos: -1, fds: fds}
}

func (r *reuseRows) Next() bool {
	r.pos++
	if r.pos >= len(r.rows) {
		return false
	}
	row := r.rows[r.pos]
	if r.buf == nil {
		r.buf = make([][]byte, len(row))
	}
	for i, v := range row {
		if v == nil {
			r.buf[i] = nil
			continue
		}
		// Rewrite the SAME backing array whenever there is one big enough —
		// the borrowed-buffer hazard, reproduced. A fresh allocation is used
		// only when there is no buffer yet or it is too small; note that an
		// empty non-NULL value still gets a non-nil buffer, because empty and
		// NULL are different values.
		if r.buf[i] == nil || cap(r.buf[i]) < len(v) {
			r.buf[i] = make([]byte, len(v))
		} else {
			r.buf[i] = r.buf[i][:len(v)]
		}
		copy(r.buf[i], v)
	}
	return true
}

func (r *reuseRows) Scan(...any) error          { return nil }
func (r *reuseRows) Close() error               { return nil }
func (r *reuseRows) Err() error                 { return nil }
func (r *reuseRows) Fields() []FieldDescription { return r.fds }

// RawValues hands back FRESH slice headers over the SAME backing arrays — pgx's
// shape, and the one that makes the reuse hazard subtle: the outer slice looks
// like a new value every row, so a consumer can hold three of them and still be
// looking at one row's bytes.
func (r *reuseRows) RawValues() [][]byte {
	out := make([][]byte, len(r.buf))
	copy(out, r.buf)
	return out
}

// plainRows implements only Rows — the capability-miss case.
type plainRows struct{}

func (plainRows) Next() bool        { return false }
func (plainRows) Scan(...any) error { return nil }
func (plainRows) Close() error      { return nil }
func (plainRows) Err() error        { return nil }

// --- the probe ---------------------------------------------------------------

// Absence is (nil, false), not an error: raw access is an optimization the
// caller falls back from. That is the deliberate difference from Columns, where
// the caller asked a question that has no answer without the capability.
func TestRawRowsOf_ProbeIsNotAnErrorPath(t *testing.T) {
	t.Parallel()

	rr, ok := RawRowsOf(newReuseRows(nil))
	if !ok || rr == nil {
		t.Error("a RawRows implementation must be probed successfully")
	}

	rr, ok = RawRowsOf(plainRows{})
	if ok {
		t.Error("a plain Rows must not satisfy RawRows")
	}
	if rr != nil {
		t.Errorf("a missed probe must yield a nil RawRows, got %v", rr)
	}
}

// --- the borrowed-buffer contract --------------------------------------------

var rawFields = []FieldDescription{
	{Name: "id", TypeOID: 23, TypeSize: 4, TypeModifier: -1, Format: 1},
	{Name: "name", TypeOID: 25, TypeSize: -1, TypeModifier: -1, Format: 0},
}

func TestRawValues_AreBorrowedUntilNext(t *testing.T) {
	t.Parallel()

	rows := func() *reuseRows {
		return newReuseRows(rawFields,
			[][]byte{{0, 0, 0, 1}, []byte("alpha")},
			[][]byte{{0, 0, 0, 2}, []byte("bravo")},
			[][]byte{{0, 0, 0, 3}, []byte("delta")},
		)
	}

	// Positive control. A consumer that keeps the driver's slices past Next
	// must observe them change — if it did not, this fake would not be
	// reproducing the hazard the contract exists for, and the copying half of
	// this test would prove nothing.
	var kept [][][]byte
	rr, _ := RawRowsOf(rows())
	for rr.Next() {
		kept = append(kept, rr.RawValues())
	}
	if len(kept) != 3 {
		t.Fatalf("read %d rows, want 3", len(kept))
	}
	if !bytes.Equal(kept[0][1], kept[2][1]) {
		t.Fatalf("the fake did not reuse its buffers (row 0 name = %q, row 2 = %q); "+
			"the copy assertions below would be vacuous", kept[0][1], kept[2][1])
	}

	// The documented consumer discipline: copy what you keep — with
	// bytes.Clone, which is the only idiom that preserves nil-vs-empty (see
	// TestRawValues_RetainedCopiesKeepNullEmptyIdentity).
	var copied [][][]byte
	rr, _ = RawRowsOf(rows())
	for rr.Next() {
		vals := rr.RawValues()
		row := make([][]byte, len(vals))
		for i, v := range vals {
			row[i] = bytes.Clone(v)
		}
		copied = append(copied, row)
	}
	want := []string{"alpha", "bravo", "delta"}
	if len(copied) != len(want) {
		t.Fatalf("read %d rows, want %d", len(copied), len(want))
	}
	for i, w := range want {
		if string(copied[i][1]) != w {
			t.Errorf("row %d name = %q, want %q", i, copied[i][1], w)
		}
	}
}

// NULL and empty are different values on the wire and must stay different:
// collapsing them would silently turn a missing value into a present one.
func TestRawValues_NullIsNilEmptyIsNotNil(t *testing.T) {
	t.Parallel()

	rr, _ := RawRowsOf(newReuseRows(rawFields,
		[][]byte{{0, 0, 0, 1}, nil},
		[][]byte{{0, 0, 0, 2}, {}},
	))

	if !rr.Next() {
		t.Fatal("no first row")
	}
	if v := rr.RawValues()[1]; v != nil {
		t.Errorf("NULL rendered as %#v, want a nil slice", v)
	}
	if !rr.Next() {
		t.Fatal("no second row")
	}
	v := rr.RawValues()[1]
	if v == nil {
		t.Error("an empty non-NULL value rendered as nil; it is not NULL")
	}
	if len(v) != 0 {
		t.Errorf("empty value has length %d", len(v))
	}
}

// The distinction has to survive the copy a consumer makes to keep the value,
// not merely exist while the row is current — a retained NULL that reads back
// as empty (or the reverse) is the same corruption arriving one step later.
//
// This is a trap with a specific shape: append([]byte(nil), v...) appends zero
// bytes to a nil destination and returns nil, so it silently turns an empty
// value into a NULL one. bytes.Clone is the idiom that holds in both
// directions, and this test is what stops the other one coming back.
func TestRawValues_RetainedCopiesKeepNullEmptyIdentity(t *testing.T) {
	t.Parallel()

	rr, _ := RawRowsOf(newReuseRows(rawFields,
		[][]byte{{0, 0, 0, 1}, nil}, // NULL
		[][]byte{{0, 0, 0, 2}, {}},  // empty, not NULL
	))

	var kept [][][]byte
	for rr.Next() {
		vals := rr.RawValues()
		row := make([][]byte, len(vals))
		for i, v := range vals {
			row[i] = bytes.Clone(v)
		}
		kept = append(kept, row)
	}
	if err := rr.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if err := rr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(kept) != 2 {
		t.Fatalf("read %d rows, want 2", len(kept))
	}

	// Both assertions are made after the stream is finished and closed: the
	// lifetime-crossing case, which is the only one a consumer actually has.
	if kept[0][1] != nil {
		t.Errorf("a retained NULL came back as %#v, want a nil slice", kept[0][1])
	}
	if kept[1][1] == nil {
		t.Error("a retained empty value came back as nil — it is not NULL")
	}
	if len(kept[1][1]) != 0 {
		t.Errorf("retained empty value has length %d", len(kept[1][1]))
	}
}

// The descriptors are the server's own, carried through unflattened: a
// pass-through consumer needs the type OID and the wire format to interpret
// RawValues at all.
func TestFields_CarryTheServerDescriptors(t *testing.T) {
	t.Parallel()

	rr, _ := RawRowsOf(newReuseRows(rawFields))
	fds := rr.Fields()
	if len(fds) != 2 {
		t.Fatalf("got %d descriptors, want 2", len(fds))
	}
	if fds[0].Name != "id" || fds[0].TypeOID != 23 || fds[0].Format != 1 {
		t.Errorf("descriptor 0 = %+v", fds[0])
	}
	if fds[1].TypeSize != -1 || fds[1].Format != 0 {
		t.Errorf("descriptor 1 = %+v", fds[1])
	}
}
