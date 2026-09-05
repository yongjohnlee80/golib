package dao

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// --- test fakes -------------------------------------------------------------

type execCall struct {
	sql  string
	args []any
}

// fakeExecer records executed statements and can fail a chosen call (1-based).
type fakeExecer struct {
	calls  []execCall
	failOn func(call int) error
}

func (f *fakeExecer) ExecContext(_ context.Context, query string, args ...any) (Result, error) {
	f.calls = append(f.calls, execCall{sql: query, args: append([]any(nil), args...)})
	if f.failOn != nil {
		if err := f.failOn(len(f.calls)); err != nil {
			return nil, err
		}
	}
	return fakeResult{}, nil
}

type fakeResult struct{}

func (fakeResult) RowsAffected() (int64, error) { return 0, nil }
func (fakeResult) LastInsertId() (int64, error) { return 0, nil }

// sqliteDialect is a SECOND dialect (MaxBindParams 999, "?" placeholders). Its
// existence proves a new dialect needs no change to the dao core (acceptance #6).
type sqliteDialect struct{ GenericDialect }

func (sqliteDialect) Name() string             { return "sqlite" }
func (sqliteDialect) MaxBindParams() int       { return 999 }
func (sqliteDialect) Placeholder(n int) string { return "?" }

func (d sqliteDialect) BuildUpsertSuffix(conflictCols, updateCols []string) string {
	return StandardUpsertSuffix(d, conflictCols, updateCols)
}

// copyDialect is a COPY-capable dialect that records its Copy calls.
type copyDialect struct {
	GenericDialect
	copies   int
	lastRows [][]any
}

// Copy makes it a dao.Copier; the boolean that used to say so is gone.
func (d *copyDialect) Copy(_ context.Context, _ any, _ string, _ []string, rows [][]any) (int64, error) {
	d.copies++
	d.lastRows = rows
	return int64(len(rows)), nil
}

// It can upsert too — which it used to inherit from GenericDialect without
// saying so. Without this it never reaches the ForceCopy-plus-conflict check,
// because the upsert gate would refuse it first.
func (d *copyDialect) BuildUpsertSuffix(conflictCols, updateCols []string) string {
	return StandardUpsertSuffix(d, conflictCols, updateCols)
}

// --- perChunkRows (the 65535 math) ------------------------------------------

func TestPerChunkRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                          string
		maxParams, cols, maxBatchRows int
		want                          int
	}{
		{"postgres 12-col", 65535, 12, 0, 5461},
		{"sqlite 3-col", 999, 3, 0, 333},
		{"pathological wide row", 10, 20, 0, 1},
		{"clamped by MaxBatchRows", 65535, 5, 100, 100},
		{"zero cols guarded", 100, 0, 0, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := perChunkRows(tt.maxParams, tt.cols, tt.maxBatchRows); got != tt.want {
				t.Errorf("perChunkRows(%d,%d,%d) = %d, want %d", tt.maxParams, tt.cols, tt.maxBatchRows, got, tt.want)
			}
		})
	}
}

func TestPerChunkRows_65535Statements(t *testing.T) {
	t.Parallel()

	// 12 columns × 100k rows: floor(65535/12)=5461 rows/chunk → ceil(100000/5461)=19.
	perChunk := perChunkRows(65535, 12, 0)
	const rows = 100_000
	statements := (rows + perChunk - 1) / perChunk
	if statements != 19 {
		t.Errorf("statements = %d, want 19", statements)
	}
	if perChunk*12 > 65535 {
		t.Errorf("chunk params %d exceed 65535", perChunk*12)
	}
}

// --- Flush behavior ---------------------------------------------------------

func TestBatch_SingleStatementWhenFits(t *testing.T) {
	t.Parallel()

	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, returningDialect{}, "t")
	b.Add(map[string]any{"a": 1, "b": 2}).Add(map[string]any{"a": 3, "b": 4})

	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	if len(ex.calls) != 1 {
		t.Fatalf("statements = %d, want 1", len(ex.calls))
	}
	if got := len(ex.calls[0].args); got != 4 {
		t.Errorf("args = %d, want 4", got)
	}
	if !strings.HasPrefix(ex.calls[0].sql, `INSERT INTO "t" ("a", "b") VALUES `) {
		t.Errorf("unexpected sql: %s", ex.calls[0].sql)
	}
}

func TestBatch_ChunksToLimit(t *testing.T) {
	t.Parallel()

	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, sqliteDialect{}, "t")
	for i := 0; i < 1000; i++ {
		b.Add(map[string]any{"a": i, "b": i, "c": i}) // 3 cols
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}

	// floor(999/3)=333 rows/chunk → ceil(1000/333)=4 statements (333,333,333,1).
	if len(ex.calls) != 4 {
		t.Fatalf("statements = %d, want 4", len(ex.calls))
	}
	total := 0
	for i, c := range ex.calls {
		if len(c.args) > 999 {
			t.Errorf("chunk %d args = %d, exceeds MaxBindParams 999", i, len(c.args))
		}
		total += len(c.args)
	}
	if total != 3000 {
		t.Errorf("total args = %d, want 3000", total)
	}
}

func TestBatch_EmptyFlushIsNoop(t *testing.T) {
	t.Parallel()

	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, returningDialect{}, "t")
	if err := b.Flush(); err != nil {
		t.Errorf("Flush() = %v, want nil", err)
	}
	if len(ex.calls) != 0 {
		t.Errorf("statements = %d, want 0", len(ex.calls))
	}
}

func TestBatch_MissingKeysBecomeNull(t *testing.T) {
	t.Parallel()

	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, returningDialect{}, "t")
	b.Add(map[string]any{"a": 1}).Add(map[string]any{"b": 2})
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	// Union cols sorted = [a, b]; row0 = (1, nil), row1 = (nil, 2).
	want := []any{1, nil, nil, 2}
	got := ex.calls[0].args
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestBatch_LenAndReset(t *testing.T) {
	t.Parallel()

	b := newBatchWriter[any, string](&fakeExecer{}, returningDialect{}, "t")
	b.Add(map[string]any{"a": 1}).SkipConflicts()
	if b.Len() != 1 {
		t.Errorf("Len() = %d, want 1", b.Len())
	}
	b.Reset()
	if b.Len() != 0 || b.skipConflict {
		t.Errorf("Reset did not clear state: len=%d skip=%v", b.Len(), b.skipConflict)
	}
}

// --- conflict suffixes ------------------------------------------------------

func TestBatch_OnConflictUpdate(t *testing.T) {
	t.Parallel()

	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, returningDialect{}, "t")
	b.Add(map[string]any{"id": 1, "name": "x"}).OnConflictUpdate("id")
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	want := `ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name"`
	if !strings.Contains(ex.calls[0].sql, want) {
		t.Errorf("sql = %q, want it to contain %q", ex.calls[0].sql, want)
	}
}

func TestBatch_SkipConflicts(t *testing.T) {
	t.Parallel()

	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, returningDialect{}, "t")
	b.Add(map[string]any{"id": 1}).SkipConflicts()
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	if !strings.Contains(ex.calls[0].sql, "ON CONFLICT DO NOTHING") {
		t.Errorf("sql = %q, want ON CONFLICT DO NOTHING", ex.calls[0].sql)
	}
}

// --- COPY fast-path ---------------------------------------------------------

func TestBatch_CopyByThreshold(t *testing.T) {
	t.Parallel()

	cd := &copyDialect{}
	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, cd, "t")
	b.copyThreshold = 2
	b.Add(map[string]any{"a": 1}).Add(map[string]any{"a": 2})
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	if cd.copies != 1 {
		t.Errorf("Copy calls = %d, want 1", cd.copies)
	}
	if len(ex.calls) != 0 {
		t.Errorf("INSERT statements = %d, want 0 (COPY path)", len(ex.calls))
	}
}

func TestBatch_ForceInsertDisablesCopy(t *testing.T) {
	t.Parallel()

	cd := &copyDialect{}
	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, cd, "t")
	b.copyThreshold = 1
	b.Add(map[string]any{"a": 1}).ForceInsert()
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	if cd.copies != 0 {
		t.Errorf("Copy calls = %d, want 0 (ForceInsert)", cd.copies)
	}
	if len(ex.calls) != 1 {
		t.Errorf("INSERT statements = %d, want 1", len(ex.calls))
	}
}

func TestBatch_ForceCopyWithConflictRejected(t *testing.T) {
	t.Parallel()

	cd := &copyDialect{}
	ex := &fakeExecer{}
	b := newBatchWriter[any, string](ex, cd, "t")
	b.Add(map[string]any{"a": 1}).ForceCopy().SkipConflicts()

	err := b.Flush()
	if err == nil || !strings.Contains(err.Error(), "COPY") {
		t.Fatalf("Flush() = %v, want an error mentioning COPY", err)
	}
	if cd.copies != 0 || len(ex.calls) != 0 {
		t.Errorf("nothing should execute on rejection: copies=%d inserts=%d", cd.copies, len(ex.calls))
	}
}

// --- chunk failure reporting ------------------------------------------------

func TestBatch_ChunkErrorReporting(t *testing.T) {
	t.Parallel()

	ex := &fakeExecer{failOn: func(call int) error {
		if call == 2 {
			return errors.New("boom")
		}
		return nil
	}}
	b := newBatchWriter[any, string](ex, sqliteDialect{}, "t")
	for i := 0; i < 1000; i++ {
		b.Add(map[string]any{"a": i, "b": i, "c": i}) // → 4 chunks
	}

	err := b.Flush()
	var be *BatchError
	if !errors.As(err, &be) {
		t.Fatalf("Flush() = %T %v, want *BatchError", err, err)
	}
	if len(be.Errors) != 1 {
		t.Fatalf("BatchError.Errors = %d, want 1", len(be.Errors))
	}
	var ce *chunkError
	if !errors.As(be.Errors[0], &ce) || ce.Index() != 1 {
		t.Errorf("want chunkError at index 1, got %v", be.Errors[0])
	}
	// Every chunk is still attempted even though chunk 1 failed.
	if len(ex.calls) != 4 {
		t.Errorf("statements attempted = %d, want 4", len(ex.calls))
	}
}

// --- AddRow wiring (the schema supplies extract) ----------------------------

func TestBatch_AddRow(t *testing.T) {
	t.Parallel()

	type artist struct{ Name, URI string }
	ex := &fakeExecer{}
	b := newBatchWriter[artist, string](ex, returningDialect{}, "artist")
	b.extract = func(a artist) map[string]any { return map[string]any{"name": a.Name, "uri": a.URI} }

	b.AddRow(artist{"a", "u1"}).AddRow(artist{"b", "u2"})
	if b.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", b.Len())
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	if len(ex.calls) != 1 || len(ex.calls[0].args) != 4 {
		t.Errorf("statements/args = %d/%d, want 1/4", len(ex.calls), len(ex.calls[0].args))
	}
}

// --- OnConflictUpdate() with no columns (the schema's declared target) ------

// TestBatch_OnConflictUpdateNoArgsUsesSchemaTarget: the no-argument form means
// "this entity's conflict target", exactly as DAO.Upsert does. Before this it
// staged nothing and silently degraded to a plain INSERT, so a re-run failed on
// the very duplicates the caller asked to update.
func TestBatch_OnConflictUpdateNoArgsUsesSchemaTarget(t *testing.T) {
	t.Parallel()

	conn := newConn() // buildSchema declares Conflict(aURI)
	s := buildSchema(conn)
	b := s.DAO().Batch().OnConflictUpdate()
	b.Add(map[artistField]any{aName: "n", aURI: "u"})
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !strings.Contains(conn.lastExec, `ON CONFLICT ("uri") DO UPDATE SET`) {
		t.Errorf("no-arg OnConflictUpdate should use the schema target: %s", conn.lastExec)
	}
	// The conflict column itself must not appear in the DO UPDATE SET list.
	if strings.Contains(conn.lastExec, `SET "uri"`) {
		t.Errorf("conflict column must be excluded from the update set: %s", conn.lastExec)
	}
}

// TestBatch_OnConflictUpdateNoArgsWithoutSchemaTargetFails: with nothing to fall
// back to, Flush refuses instead of emitting a plain INSERT.
func TestBatch_OnConflictUpdateNoArgsWithoutSchemaTargetFails(t *testing.T) {
	t.Parallel()

	conn := newConn()
	// A schema with no Conflict(...) option: same fields, no conflict target.
	s := New(conn,
		Table[*artist, artistField, artistSort, string]("artist"),
		ID[*artist, artistField, artistSort, string](aID),
		Fields[*artist, artistField, artistSort, string](artistFields),
		Default[*artist, artistField, artistSort, string](aID, aName),
		// artistFields declares a join, so it must still be registered; only
		// the Conflict(...) option is missing here.
		OptionalJoin[*artist, artistField, artistSort, string]("label_group",
			"LEFT JOIN label_group ON label_group.id = artist.label_group_id"),
	)
	b := s.DAO().Batch().OnConflictUpdate()
	b.Add(map[artistField]any{aName: "n"})
	err := b.Flush()
	if !errors.Is(err, ErrNoConflictTarget) {
		t.Fatalf("Flush err = %v, want ErrNoConflictTarget", err)
	}
	for _, want := range []string{"artist", "Conflict"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
	if conn.lastExec != "" {
		t.Errorf("nothing may execute: %s", conn.lastExec)
	}
}

// Explicit columns keep winning, and still work when the schema declares a
// different target.
func TestBatch_OnConflictUpdateExplicitOverridesSchemaTarget(t *testing.T) {
	t.Parallel()

	conn := newConn() // schema target is aURI
	s := buildSchema(conn)
	b := s.DAO().Batch().OnConflictUpdate(aName)
	b.Add(map[artistField]any{aName: "n", aURI: "u"})
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !strings.Contains(conn.lastExec, `ON CONFLICT ("name") DO UPDATE SET`) {
		t.Errorf("explicit columns must win: %s", conn.lastExec)
	}
}

// The COPY fast path cannot express conflict handling, and the no-argument form
// is conflict handling — so it must be rejected exactly like the explicit one.
func TestBatch_ForceCopyWithSchemaConflictRejected(t *testing.T) {
	t.Parallel()

	conn := newConn() // declares Conflict(aURI)
	s := buildSchema(conn)
	b := s.DAO().Batch().ForceCopy().OnConflictUpdate()
	b.Add(map[artistField]any{aName: "n", aURI: "u"})
	if err := b.Flush(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Flush err = %v, want ErrUnsupported (COPY cannot upsert)", err)
	}
}
