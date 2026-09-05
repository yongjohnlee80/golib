package dao

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// defaultCopyThreshold is the staged-row count at or above which a COPY-capable
// dialect uses its bulk-load fast-path instead of chunked INSERT.
const defaultCopyThreshold = 1000

// BatchWriter accumulates rows and flushes them as the minimum number of
// statements that respect the dialect's bind-parameter limit, with an optional
// COPY fast-path. Obtain one from a DAO via Batch. Chunking is automatic and
// driver-aware: the caller never reasons about bind-parameter limits.
type BatchWriter[R any, C ~string] interface {
	// Add stages one row's column values. Columns may vary across rows; the union
	// of all staged keys determines the INSERT column list, and a row missing a
	// key contributes NULL for it.
	Add(values map[C]any) BatchWriter[R, C]

	// AddRow stages a row from a model value using the schema's write fields.
	AddRow(r R) BatchWriter[R, C]

	// SkipConflicts appends the dialect's "do nothing on conflict" clause.
	SkipConflicts() BatchWriter[R, C]

	// OnConflictUpdate upserts the staged columns on conflict with conflictCols.
	// Called with NO columns it uses the schema's declared Conflict(...) target,
	// exactly as DAO.Upsert does — and when the schema declares none, Flush
	// returns an error rather than degrading to a plain INSERT.
	OnConflictUpdate(conflictCols ...C) BatchWriter[R, C]

	// ForceCopy forces the COPY fast-path (where supported); it cannot be
	// combined with conflict handling.
	ForceCopy() BatchWriter[R, C]

	// ForceInsert forces the chunked-INSERT path even for large batches.
	ForceInsert() BatchWriter[R, C]

	// Flush writes all staged rows. It returns a *BatchError (whose Unwrap
	// returns the per-chunk errors) if any chunk fails; other chunks are still
	// attempted.
	Flush() error

	// Len reports the number of staged rows.
	Len() int

	// Reset clears the staged rows and conflict/copy options.
	Reset()
}

// batchWriter is the concrete BatchWriter. The schema/factory (ADR-0006) supplies
// the table, the field-to-column resolver (colName), the row extractor for
// AddRow (extract), the error translator, and the debug log hook; until then the
// constructor defaults make it fully usable via Add.
type batchWriter[R any, C ~string] struct {
	ctx           context.Context
	exec          Execer
	dialect       Dialect
	table         string
	translate     func(error) error
	pipe          func(op Op) *pipeline
	colName       func(C) string
	extract       func(R) map[C]any
	copyThreshold int

	rows         []map[C]any
	skipConflict bool
	conflictCols []C
	// useSchemaConflict records an OnConflictUpdate() called with no columns:
	// the intent is "this entity's declared conflict target".
	useSchemaConflict bool
	// schemaConflict is the schema's resolved Conflict(...) columns, wired by
	// the DAO so the no-argument form means the same thing as DAO.Upsert.
	schemaConflict []string
	forceCopy      bool
	forceInsert    bool

	// initErr, when set, is returned by Flush before any work — used when a
	// tx-bound Batch could not resolve its transaction executor.
	initErr error
}

// newBatchWriter builds a batchWriter with safe defaults: identity column
// resolution (the field key is its own column), no-op translate/log, and the
// default COPY threshold. The schema wires the real resolvers.
func newBatchWriter[R any, C ~string](exec Execer, dialect Dialect, table string) *batchWriter[R, C] {
	return &batchWriter[R, C]{
		ctx:           context.Background(),
		exec:          exec,
		dialect:       dialect,
		table:         table,
		translate:     func(e error) error { return e },
		pipe:          func(Op) *pipeline { return nil },
		colName:       func(c C) string { return string(c) },
		copyThreshold: defaultCopyThreshold,
	}
}

func (b *batchWriter[R, C]) Add(values map[C]any) BatchWriter[R, C] {
	b.rows = append(b.rows, values)
	return b
}

func (b *batchWriter[R, C]) AddRow(r R) BatchWriter[R, C] {
	if b.extract == nil {
		panic("dao: AddRow requires a schema-built BatchWriter (ADR-0006 wires the row extractor); use Add(map[C]any) until then")
	}
	return b.Add(b.extract(r))
}

func (b *batchWriter[R, C]) SkipConflicts() BatchWriter[R, C] {
	b.skipConflict = true
	return b
}

func (b *batchWriter[R, C]) OnConflictUpdate(conflictCols ...C) BatchWriter[R, C] {
	if len(conflictCols) == 0 {
		// "Upsert on this entity's conflict target" — resolved at flush time,
		// where a missing declaration can be reported instead of silently
		// becoming a plain INSERT.
		b.useSchemaConflict = true
		return b
	}
	b.conflictCols = append(b.conflictCols, conflictCols...)
	return b
}

func (b *batchWriter[R, C]) ForceCopy() BatchWriter[R, C] {
	b.forceCopy = true
	return b
}

func (b *batchWriter[R, C]) ForceInsert() BatchWriter[R, C] {
	b.forceInsert = true
	return b
}

func (b *batchWriter[R, C]) Len() int { return len(b.rows) }

func (b *batchWriter[R, C]) Reset() {
	b.rows = nil
	b.skipConflict = false
	b.conflictCols = nil
	b.forceCopy = false
	b.forceInsert = false
}

// Flush writes all staged rows. With no rows it is a no-op. It chooses the COPY
// fast-path when shouldCopy allows, otherwise it emits chunked multi-row INSERTs
// sized to the dialect's limits.
func (b *batchWriter[R, C]) Flush() error {
	if b.initErr != nil {
		return b.initErr
	}
	// A no-argument OnConflictUpdate needs a declared target. Refusing here is
	// the point: silently emitting a plain INSERT would make a re-run fail on
	// the very duplicates the caller asked to update (the trap the package's
	// "never silently degrade conflict handling" rule names).
	if b.useSchemaConflict && len(b.conflictCols) == 0 && len(b.schemaConflict) == 0 {
		return fmt.Errorf("%w: OnConflictUpdate() with no columns needs the schema's "+
			"Conflict(...) option, which %q does not declare (name the columns, or declare Conflict)",
			ErrNoConflictTarget, b.table)
	}
	if len(b.rows) == 0 {
		return nil
	}
	// Capability gates (ADR-0008 §2.4/§2.5). An explicit ForceCopy on a dialect
	// that cannot COPY is ErrUnsupported, and this capability gate wins over the
	// combination check below (nit #4). Conflict handling on a no-upsert dialect is
	// likewise ErrUnsupported, never a silent plain-INSERT that drops the clause.
	copier, canCopy := b.dialect.(Copier)
	if b.forceCopy && !canCopy {
		return fmt.Errorf("%w: COPY", ErrUnsupported)
	}
	if _, canUpsert := b.dialect.(Upserter); b.hasConflictHandling() && !canUpsert {
		return fmt.Errorf("%w: upsert (batch conflict handling)", ErrUnsupported)
	}
	if b.forceCopy && b.hasConflictHandling() {
		return errors.New("dao: ForceCopy cannot be combined with conflict handling (COPY cannot express upsert/skip)")
	}

	keys, cols := b.keysAndCols()
	matrix := b.matrix(keys)

	if b.shouldCopy(len(matrix), len(cols)) {
		// Observe-only hook event (ADR-0009 §2.6): COPY has no SQL statement;
		// a hook that mutates the synthetic descriptor fails the flush, and a
		// hook error vetoes the COPY per the ordinary abort rule.
		pl := b.pipe(OpBatchCopy)
		desc := fmt.Sprintf("COPY %s (%s) — %d rows", b.table, strings.Join(cols, ", "), len(matrix))
		if err := pl.beforeExecFrozen(desc); err != nil {
			return err
		}
		n, err := copier.Copy(b.ctx, b.exec, b.table, cols, matrix)
		return pl.finish(0, n, b.translate(err))
	}

	perChunk := perChunkRows(b.dialect.MaxBindParams(), len(cols), b.dialect.MaxBatchRows())
	suffix := b.suffix(cols)

	var errs []error
	for i := 0; i < len(matrix); i += perChunk {
		chunk := matrix[i:min(i+perChunk, len(matrix))]
		bld := &builder{dialect: b.dialect}
		sqlText := bld.buildBatchInsert(b.table, cols, chunk, suffix)
		args := bld.args
		// Each chunk is a real statement: the full rewrite contract applies
		// (Op: OpBatch, ADR-0009 §2.6).
		pl := b.pipe(OpBatch)
		if err := pl.beforeExec(&sqlText, &args); err != nil {
			errs = append(errs, &chunkError{index: i / perChunk, err: err})
			continue
		}
		res, err := b.exec.ExecContext(b.ctx, sqlText, args...)
		if ferr := pl.finish(0, affectedOf(res), b.translate(err)); ferr != nil {
			errs = append(errs, &chunkError{index: i / perChunk, err: ferr})
		}
	}
	if len(errs) > 0 {
		return &BatchError{Errors: errs}
	}
	return nil
}

func (b *batchWriter[R, C]) hasConflictHandling() bool {
	return b.skipConflict || len(b.conflictCols) > 0 || b.useSchemaConflict
}

// keysAndCols returns the sorted union of staged field keys and the column names
// they resolve to, aligned positionally. Sorting keeps emitted SQL stable.
func (b *batchWriter[R, C]) keysAndCols() (keys []C, cols []string) {
	seen := make(map[C]struct{})
	for _, row := range b.rows {
		for k := range row {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				keys = append(keys, k)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	cols = make([]string, len(keys))
	for i, k := range keys {
		cols[i] = b.colName(k)
	}
	return keys, cols
}

// matrix projects the staged rows onto keys, in order, filling absent keys with nil.
func (b *batchWriter[R, C]) matrix(keys []C) [][]any {
	m := make([][]any, len(b.rows))
	for i, row := range b.rows {
		vals := make([]any, len(keys))
		for j, k := range keys {
			vals[j] = row[k]
		}
		m[i] = vals
	}
	return m
}

// shouldCopy reports whether the COPY fast-path applies. COPY cannot express
// conflict handling, so any skip/upsert disqualifies it.
func (b *batchWriter[R, C]) shouldCopy(nrows, _ int) bool {
	if _, canCopy := b.dialect.(Copier); !canCopy || b.forceInsert {
		return false
	}
	if b.hasConflictHandling() {
		return false
	}
	if b.forceCopy {
		return true
	}
	return nrows >= b.copyThreshold
}

// suffix renders the ON CONFLICT clause for the staged conflict options, or "".
func (b *batchWriter[R, C]) suffix(cols []string) string {
	switch {
	case len(b.conflictCols) == 0 && b.useSchemaConflict:
		// OnConflictUpdate() with no columns: the schema's declared target.
		// Flush has already rejected the case where there is none.
		conflict := b.schemaConflict
		return b.upsertSuffix(conflict, subtract(cols, conflict))
	case len(b.conflictCols) > 0:
		conflict := make([]string, len(b.conflictCols))
		for i, c := range b.conflictCols {
			conflict[i] = b.colName(c)
		}
		return b.upsertSuffix(conflict, subtract(cols, conflict))
	case b.skipConflict:
		// The insert columns ride along as a hint for dialects (MySQL) that
		// cannot express "do nothing" without naming a column; suffix-complete
		// dialects ignore them (ADR-0011 §2.3).
		return b.upsertSuffix(nil, cols)
	}
	return ""
}

// perChunkRows is the number of rows per batch statement: floor(maxParams/cols),
// at least one (a single pathologically wide row still emits), clamped to
// maxBatchRows when that is set.
func perChunkRows(maxParams, cols, maxBatchRows int) int {
	pc := maxParams / max(cols, 1)
	if pc < 1 {
		pc = 1
	}
	if maxBatchRows > 0 && pc > maxBatchRows {
		pc = maxBatchRows
	}
	return pc
}

// subtract returns the elements of all that are not in remove, preserving order.
func subtract(all, remove []string) []string {
	rm := make(map[string]struct{}, len(remove))
	for _, r := range remove {
		rm[r] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for _, a := range all {
		if _, ok := rm[a]; !ok {
			out = append(out, a)
		}
	}
	return out
}

// BatchError reports that one or more chunks of a [BatchWriter.Flush] failed. Its
// Unwrap returns the per-chunk errors (each a *chunkError identifying the chunk).
type BatchError struct {
	Errors []error
}

// Error implements error.
func (e *BatchError) Error() string {
	return fmt.Sprintf("dao: batch flush failed in %d chunk(s): %v", len(e.Errors), errors.Join(e.Errors...))
}

// Unwrap returns the per-chunk errors for errors.Is/As traversal.
func (e *BatchError) Unwrap() []error { return e.Errors }

// chunkError identifies a single failed chunk within a batch flush.
type chunkError struct {
	index int
	err   error
}

// Error implements error.
func (e *chunkError) Error() string { return fmt.Sprintf("chunk %d: %v", e.index, e.err) }

// Unwrap returns the underlying (already-translated) chunk error.
func (e *chunkError) Unwrap() error { return e.err }

// Index reports which chunk (0-based) failed.
func (e *chunkError) Index() int { return e.index }

// upsertSuffix renders the conflict clause when the dialect can upsert. An
// engine that cannot has already been refused by the capability gate above, so
// reaching here without one would be a bug; the empty string keeps the
// statement a plain INSERT rather than emitting a clause no engine asked for.
func (b *batchWriter[R, C]) upsertSuffix(conflict, updateCols []string) string {
	u, ok := b.dialect.(Upserter)
	if !ok {
		return ""
	}
	return u.BuildUpsertSuffix(conflict, updateCols)
}
