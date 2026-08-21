package dao

import "strings"

// JoinKey names an optional join registered on a [Schema] (via dao.OptionalJoin,
// ADR-0006). A [Field] carrying a non-empty Join triggers that join whenever the
// field is selected, ordered-by, or filtered on (ADR-0003 resolves them).
type JoinKey string

// Field is the complete declaration of one selectable/writable column. Declaring
// the column expression, the scan target, and the optional join trigger together
// is what eliminates the prior art's threefold restatement of each column
// (enum / translator / scanner).
type Field[R any] struct {
	// Column is the SQL expression projected for this field, fully qualified —
	// e.g. "artist.name" or "COALESCE(label_group.name,'')". For writes, the bare
	// column name is derived from it (see WriteColumn / writeCol).
	Column string

	// Expr is the dialect-resolved alternative to Column (ADR-0016): [New]
	// renders it once against the connection's dialect and stores the result as
	// Column, and — unless WriteColumn is set explicitly — takes the raw write
	// identity from it, so everything downstream is byte-identical to a
	// hand-written declaration. Build one with [T], [C], [Coalesce] and friends.
	//
	// Setting both Column and Expr is a declaration error: [New] panics.
	Expr Expr

	// Scan returns a pointer into the row struct for this field, called with a
	// freshly-allocated R during scanning. Nil for write-only fields.
	Scan func(R) any

	// Value returns this field's value from a model, used by BatchWriter.AddRow to
	// turn a model R into staged column values. Leave nil for fields that should
	// not be written from a model (e.g. a DB-generated id) or that are ReadOnly.
	//
	// (This closes the gap where Field carried only a read-side Scan: AddRow needs
	// the inverse, a model->value extractor.)
	Value func(R) any

	// Join, if non-empty, names an optional join that must be applied whenever
	// this field is selected, ordered-by, or filtered on. Empty means no join.
	Join JoinKey

	// ReadOnly marks computed/joined fields that must never appear in a write.
	ReadOnly bool

	// WriteColumn overrides the bare column name used for writes when Column is a
	// qualified or expression form (e.g. Column "artist.name" → WriteColumn
	// "name"). Optional; defaults to the unqualified tail of Column.
	WriteColumn string

	// Clearable declares that a rules-driven Clear (SetRules, ADR-0010) may
	// target this column. It is a deliberate per-column decision — never
	// inferred from the Go field's nilability (a nilable column without this
	// flag is not clearable; a NOT NULL column can be Clearable via ClearValue).
	Clearable bool

	// ClearValue is what a clear writes when Clearable is true: nil (the
	// default) writes SQL NULL; a non-nil value is the cleared-state sentinel
	// for a NOT NULL column (e.g. a date sentinel). Setting ClearValue with
	// Clearable false is a declaration error rejected by dao.New.
	ClearValue any
}

// writeCol returns the bare column name to use in INSERT/UPDATE: WriteColumn when
// set, otherwise the unqualified tail of Column (the part after the last dot).
// It is only meaningful for writable fields; ReadOnly fields are never written.
func (f Field[R]) writeCol() string {
	if f.WriteColumn != "" {
		return f.WriteColumn
	}
	col := f.Column
	if i := strings.LastIndexByte(col, '.'); i >= 0 {
		col = col[i+1:]
	}
	return col
}
