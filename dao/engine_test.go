package dao

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/logger"
)

// --- fake DataConn ----------------------------------------------------------

type fakeConn struct {
	d         Dialect
	lastQuery string
	lastArgs  []any
	lastExec  string
	lastEArgs []any
	rows      *fakeRows
	queryErr  error
	execErr   error
}

func (c *fakeConn) QueryContext(_ context.Context, q string, args ...any) (Rows, error) {
	c.lastQuery, c.lastArgs = q, args
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	if c.rows == nil {
		return &fakeRows{}, nil
	}
	return c.rows, nil
}

func (c *fakeConn) ExecContext(_ context.Context, q string, args ...any) (Result, error) {
	c.lastExec, c.lastEArgs = q, args
	if c.execErr != nil {
		return nil, c.execErr
	}
	return fakeResult{}, nil
}

func (c *fakeConn) Dialect() Dialect                      { return c.d }
func (c *fakeConn) Begin(context.Context) (TxConn, error) { return nil, errors.New("no tx") }
func (c *fakeConn) Name() string                          { return "fake" }
func (c *fakeConn) Close() error                          { return nil }

type fakeRows struct {
	data [][]any
	i    int
	err  error
}

func (r *fakeRows) Next() bool {
	if r.err != nil || r.i >= len(r.data) {
		return false
	}
	r.i++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.data[r.i-1]
	for j, d := range dest {
		if j >= len(row) || row[j] == nil {
			continue
		}
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(row[j]))
	}
	return nil
}

func (r *fakeRows) Close() error { return nil }
func (r *fakeRows) Err() error   { return r.err }

// --- entity under test ------------------------------------------------------

type artist struct {
	ID, Name, URI, LabelGroup string
	Public                    bool
}

type artistField string

const (
	aID         artistField = "id"
	aName       artistField = "name"
	aURI        artistField = "uri"
	aLabelGroup artistField = "label_group_name"
	aPublic     artistField = "public"
)

type artistSort string

const aSortName artistSort = "name"

var artistFields = map[artistField]Field[*artist]{
	aID:   {Column: "artist.id", Scan: func(a *artist) any { return &a.ID }},
	aName: {Column: "artist.name", Scan: func(a *artist) any { return &a.Name }, Value: func(a *artist) any { return a.Name }},
	aURI:  {Column: "artist.uri", Scan: func(a *artist) any { return &a.URI }, Value: func(a *artist) any { return a.URI }},
	aLabelGroup: {Column: "COALESCE(label_group.name,'')", Join: "label_group", ReadOnly: true,
		Scan: func(a *artist) any { return &a.LabelGroup }},
	aPublic: {Column: "artist.public", Scan: func(a *artist) any { return &a.Public }, Value: func(a *artist) any { return a.Public }},
}

func buildSchema(conn DataConn, extra ...Option[*artist, artistField, artistSort, string]) *Schema[*artist, artistField, artistSort, string] {
	base := []Option[*artist, artistField, artistSort, string]{
		Table[*artist, artistField, artistSort, string]("artist"),
		ID[*artist, artistField, artistSort, string](aID),
		Fields[*artist, artistField, artistSort, string](artistFields),
		Default[*artist, artistField, artistSort, string](aID, aName, aURI),
		OptionalJoin[*artist, artistField, artistSort, string]("label_group",
			"LEFT JOIN label_group ON label_group.id = artist.label_group_id"),
		SortMap[*artist, artistField, artistSort, string](map[artistSort]string{aSortName: "artist.name"}),
		Conflict[*artist, artistField, artistSort, string](aURI),
	}
	return New(conn, append(base, extra...)...)
}

// returningDialect is what these tests actually mean by "a dialect": Postgres
// shaped SQL that can RETURNING and upsert. GenericDialect provides the SHAPE and
// deliberately implements no CAPABILITY, so a test needing one declares it —
// the same rule the production dialects follow. Before capabilities existed
// this test suite inherited RETURNING from GenericDialect without saying so,
// which is precisely the silent-inheritance the split removes.
type returningDialect struct{ GenericDialect }

func (returningDialect) ReturningClause(quotedIDCol string) string {
	return StandardReturningClause(quotedIDCol)
}

// ...and Upserter, for the same reason: ON CONFLICT used to arrive by
// promotion from GenericDialect, so the suite never had to say it wanted an
// engine that can upsert. Now it does.
func (d returningDialect) BuildUpsertSuffix(conflictCols, updateCols []string) string {
	return StandardUpsertSuffix(d, conflictCols, updateCols)
}

func newConn() *fakeConn { return &fakeConn{d: returningDialect{}} }

// --- reads ------------------------------------------------------------------

func TestSelect_ProjectionAndScan(t *testing.T) {
	t.Parallel()

	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{"1", "Alpha", "alpha"}, {"2", "Beta", "beta"}}}
	s := buildSchema(conn)

	got, err := s.DAO().Select(aID, aName, aURI)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	wantSQL := `SELECT artist.id, artist.name, artist.uri FROM "artist"`
	if conn.lastQuery != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastQuery, wantSQL)
	}
	if len(got) != 2 || got[0].Name != "Alpha" || got[1].URI != "beta" {
		t.Errorf("scanned rows wrong: %+v", got)
	}
}

func TestSelect_DefaultColumns(t *testing.T) {
	t.Parallel()

	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{"1", "A", "a"}}}
	s := buildSchema(conn)

	if _, err := s.DAO().Select(); err != nil {
		t.Fatalf("Select: %v", err)
	}
	// Default set is aID, aName, aURI.
	if conn.lastQuery != `SELECT artist.id, artist.name, artist.uri FROM "artist"` {
		t.Errorf("default-cols sql = %q", conn.lastQuery)
	}
}

func TestSelect_AllColumnsWhenNoDefault(t *testing.T) {
	t.Parallel()

	conn := newConn()
	conn.rows = &fakeRows{}
	// No Default option → all fields, in sorted key order.
	s := New(conn,
		Table[*artist, artistField, artistSort, string]("artist"),
		ID[*artist, artistField, artistSort, string](aID),
		Fields[*artist, artistField, artistSort, string](artistFields),
		OptionalJoin[*artist, artistField, artistSort, string]("label_group", "LEFT JOIN label_group ON x"),
	)
	if _, err := s.DAO().Select(); err != nil {
		t.Fatalf("Select: %v", err)
	}
	// Sorted keys: id, label_group_name, name, public, uri.
	for _, want := range []string{"artist.id", "COALESCE(label_group.name,'')", "artist.name", "artist.public", "artist.uri"} {
		if !strings.Contains(conn.lastQuery, want) {
			t.Errorf("all-cols sql %q missing %q", conn.lastQuery, want)
		}
	}
}

func TestJoins_OnDemand(t *testing.T) {
	t.Parallel()

	// Non-join columns → no join.
	conn := newConn()
	conn.rows = &fakeRows{}
	buildSchema(conn).DAO().Select(aID, aName)
	if strings.Contains(conn.lastQuery, "JOIN") {
		t.Errorf("unexpected join: %q", conn.lastQuery)
	}

	// A join-triggering column → exactly one join.
	conn2 := newConn()
	conn2.rows = &fakeRows{}
	buildSchema(conn2).DAO().Select(aID, aLabelGroup)
	if strings.Count(conn2.lastQuery, "LEFT JOIN") != 1 {
		t.Errorf("want exactly one join, got %q", conn2.lastQuery)
	}

	// Forced + column-triggered same key → still one join (dedup).
	conn3 := newConn()
	conn3.rows = &fakeRows{}
	buildSchema(conn3).DAO().Join("label_group").Select(aID, aLabelGroup)
	if strings.Count(conn3.lastQuery, "LEFT JOIN") != 1 {
		t.Errorf("dedup failed: %q", conn3.lastQuery)
	}
}

func TestGet_WhereAndLimit(t *testing.T) {
	t.Parallel()

	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{"7", "G", "g"}}}
	got, err := buildSchema(conn).DAO().With(aURI, "g").Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	wantSQL := `SELECT artist.id, artist.name, artist.uri FROM "artist" WHERE artist.uri = $1 LIMIT $2`
	if conn.lastQuery != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastQuery, wantSQL)
	}
	if !reflect.DeepEqual(conn.lastArgs, []any{"g", int64(1)}) {
		t.Errorf("args = %#v", conn.lastArgs)
	}
	if got.ID != "7" {
		t.Errorf("scanned %+v", got)
	}
}

func TestGet_NoRows(t *testing.T) {
	t.Parallel()

	conn := newConn()
	conn.rows = &fakeRows{} // empty
	_, err := buildSchema(conn).DAO().With(aID, "nope").Get()
	if !errors.Is(err, ErrNoRows) {
		t.Errorf("err = %v, want ErrNoRows", err)
	}
}

func TestCountAndExists(t *testing.T) {
	t.Parallel()

	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{uint64(5)}}}
	n, err := buildSchema(conn).DAO().With(aPublic, true).Count()
	if err != nil || n != 5 {
		t.Errorf("Count = %d, %v", n, err)
	}
	if conn.lastQuery != `SELECT COUNT(*) FROM "artist" WHERE artist.public = $1` {
		t.Errorf("count sql = %q", conn.lastQuery)
	}

	conn2 := newConn()
	conn2.rows = &fakeRows{data: [][]any{{true}}}
	ok, err := buildSchema(conn2).DAO().With(aID, "1").Exists()
	if err != nil || !ok {
		t.Errorf("Exists = %v, %v", ok, err)
	}
	if !strings.HasPrefix(conn2.lastQuery, "SELECT EXISTS(SELECT 1 FROM ") {
		t.Errorf("exists sql = %q", conn2.lastQuery)
	}
}

// --- writes -----------------------------------------------------------------

func TestInsert_Returning(t *testing.T) {
	t.Parallel()

	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{"new-id"}}}
	id, err := buildSchema(conn).DAO().Set(aName, "x").Set(aURI, "u").Insert()
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id != "new-id" {
		t.Errorf("id = %q, want new-id", id)
	}
	wantSQL := `INSERT INTO "artist" ("name", "uri") VALUES ($1, $2) RETURNING "id"`
	if conn.lastQuery != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastQuery, wantSQL)
	}
}

func TestInsert_NothingStaged(t *testing.T) {
	t.Parallel()

	if _, err := buildSchema(newConn()).DAO().Insert(); !errors.Is(err, ErrNothingToInsert) {
		t.Errorf("err = %v, want ErrNothingToInsert", err)
	}
}

func TestUpsert(t *testing.T) {
	t.Parallel()

	conn := newConn()
	err := buildSchema(conn).DAO().Set(aName, "x").Set(aURI, "u").Upsert()
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	want := `INSERT INTO "artist" ("name", "uri") VALUES ($1, $2) ` +
		`ON CONFLICT ("uri") DO UPDATE SET "name" = EXCLUDED."name"`
	if conn.lastExec != want {
		t.Errorf("sql = %q, want %q", conn.lastExec, want)
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	conn := newConn()
	err := buildSchema(conn).DAO().With(aID, "1").Set(aName, "x").Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantSQL := `UPDATE "artist" SET "name" = $1 WHERE artist.id = $2`
	if conn.lastExec != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastExec, wantSQL)
	}
	if !reflect.DeepEqual(conn.lastEArgs, []any{"x", "1"}) {
		t.Errorf("args = %#v", conn.lastEArgs)
	}
}

func TestUpdate_NoConditions(t *testing.T) {
	t.Parallel()

	if err := buildSchema(newConn()).DAO().Set(aName, "x").Update(); !errors.Is(err, ErrNoConditions) {
		t.Errorf("err = %v, want ErrNoConditions", err)
	}
}

func TestUpdate_JoinSubselect(t *testing.T) {
	t.Parallel()

	conn := newConn()
	err := buildSchema(conn).DAO().With(aID, "1").Set(aName, "x").Join("label_group").Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := `UPDATE "artist" SET "name" = $1 WHERE "id" IN (SELECT "artist"."id" FROM "artist" ` +
		`LEFT JOIN label_group ON label_group.id = artist.label_group_id WHERE artist.id = $2)`
	if conn.lastExec != want {
		t.Errorf("sql = %q\nwant %q", conn.lastExec, want)
	}
}

func TestDelete_NoConditions(t *testing.T) {
	t.Parallel()

	if err := buildSchema(newConn()).DAO().Delete(); !errors.Is(err, ErrNoConditions) {
		t.Errorf("err = %v, want ErrNoConditions", err)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	conn := newConn()
	if err := buildSchema(conn).DAO().With(aID, "9").Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if conn.lastExec != `DELETE FROM "artist" WHERE artist.id = $1` {
		t.Errorf("sql = %q", conn.lastExec)
	}
}

// --- fail-fast & read-only --------------------------------------------------

func TestUnknownField_FailsFast(t *testing.T) {
	t.Parallel()

	conn := newConn()
	_, err := buildSchema(conn).DAO().With(artistField("bogus"), 1).Get()
	if !errors.Is(err, ErrUnknownField) {
		t.Errorf("err = %v, want ErrUnknownField", err)
	}
	if conn.lastQuery != "" {
		t.Errorf("no SQL should execute on a staged error, got %q", conn.lastQuery)
	}
}

func TestBatch_AddRowFromModel(t *testing.T) {
	t.Parallel()

	conn := newConn()
	// A schema-built batch wires the AddRow extractor from each Field.Value.
	b := buildSchema(conn).DAO().Batch().ForceInsert()
	b.AddRow(&artist{Name: "a", URI: "u1"}).AddRow(&artist{Name: "b", URI: "u2"})
	if b.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", b.Len())
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(conn.lastExec) == 0 {
		t.Fatal("no INSERT emitted")
	}
	// Writable fields with a Value func: name, public, uri (id has none; label is ReadOnly).
	for _, col := range []string{`"name"`, `"public"`, `"uri"`} {
		if !strings.Contains(conn.lastExec, col) {
			t.Errorf("AddRow INSERT %q missing column %s", conn.lastExec, col)
		}
	}
	if strings.Contains(conn.lastExec, `"id"`) || strings.Contains(conn.lastExec, "label_group") {
		t.Errorf("AddRow should not write id or the read-only join field: %q", conn.lastExec)
	}
}

func TestReadOnlyField_StagedError(t *testing.T) {
	t.Parallel()

	conn := newConn()
	_, err := buildSchema(conn).DAO().Set(aLabelGroup, "x").Insert()
	if !errors.Is(err, ErrReadOnlyField) {
		t.Errorf("err = %v, want ErrReadOnlyField", err)
	}
	if conn.lastQuery != "" || conn.lastExec != "" {
		t.Errorf("no SQL should execute on a staged error")
	}
}

// --- error translation ------------------------------------------------------

// translatingDialect declares Returner because this test drives the RETURNING
// path deliberately — its fake reports queryErr, which only the RETURNING
// branch reaches. Before capabilities it inherited that from GenericDialect
// without saying so.
type translatingDialect struct{ GenericDialect }

func (translatingDialect) ReturningClause(quotedIDCol string) string {
	return StandardReturningClause(quotedIDCol)
}

func (translatingDialect) TranslateError(err error) error {
	if err != nil && err.Error() == "dup" {
		return &ConstraintError{Constraint: "artist_uri_key", Kind: Unique, Err: ErrDuplicate}
	}
	return err
}

func TestErrorTranslation_MappedAndDefault(t *testing.T) {
	t.Parallel()

	mapped := errors.New("artist uri already taken")

	// With an ErrorMap entry for the constraint name → mapped error.
	conn := &fakeConn{d: translatingDialect{}, queryErr: errors.New("dup")}
	s := buildSchema(conn, Errors[*artist, artistField, artistSort, string](ErrorMap{"artist_uri_key": mapped}))
	if _, err := s.DAO().Set(aName, "x").Set(aURI, "u").Insert(); !errors.Is(err, mapped) {
		t.Errorf("err = %v, want mapped", err)
	}

	// Without a mapping → the ConstraintError (still Is ErrDuplicate).
	conn2 := &fakeConn{d: translatingDialect{}, queryErr: errors.New("dup")}
	if _, err := buildSchema(conn2).DAO().Set(aName, "x").Set(aURI, "u").Insert(); !errors.Is(err, ErrDuplicate) {
		t.Errorf("err = %v, want Is ErrDuplicate", err)
	}
}

// --- builder validation & determinism --------------------------------------

func TestNew_PanicsOnMissingRequired(t *testing.T) {
	t.Parallel()

	cases := map[string]func(){
		"no table": func() {
			New(newConn(), Fields[*artist, artistField, artistSort, string](artistFields))
		},
		"no fields": func() {
			New(newConn(), Table[*artist, artistField, artistSort, string]("artist"))
		},
		"nil conn": func() {
			New[*artist, artistField, artistSort, string](nil,
				Table[*artist, artistField, artistSort, string]("artist"),
				Fields[*artist, artistField, artistSort, string](artistFields))
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			fn()
		})
	}
}

func TestDeterministicSQL(t *testing.T) {
	t.Parallel()

	run := func() string {
		conn := newConn()
		conn.rows = &fakeRows{}
		buildSchema(conn).DAO().With(aPublic, true).With(aName, "x").OrderBy(Asc(aSortName)).Select(aID, aName, aURI)
		return conn.lastQuery
	}
	if a, b := run(), run(); a != b {
		t.Errorf("SQL not deterministic:\n %q\n %q", a, b)
	}
}

// --- logger -----------------------------------------------------------------

type capLogger struct{ n int }

func (c *capLogger) Log(logger.Severity, any) { c.n++ }

func TestDebugLogging(t *testing.T) {
	t.Parallel()

	// Debug off (default) → no logging.
	off := &capLogger{}
	connOff := newConn()
	connOff.rows = &fakeRows{}
	buildSchema(connOff, WithLogger[*artist, artistField, artistSort, string](off)).DAO().Select(aID)
	if off.n != 0 {
		t.Errorf("debug off logged %d times", off.n)
	}

	// Debug on → one log per statement.
	on := &capLogger{}
	connOn := newConn()
	connOn.rows = &fakeRows{}
	buildSchema(connOn,
		WithLogger[*artist, artistField, artistSort, string](on),
		Debug[*artist, artistField, artistSort, string](true),
	).DAO().Select(aID)
	if on.n != 1 {
		t.Errorf("debug on logged %d times, want 1", on.n)
	}
}
