package dao

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// --- entity under test: a release with the three clearability postures ------

type release struct {
	ID, Title, UPC, Grid, Date, Notes string
}

type relField string

const (
	rID    relField = "id"
	rTitle relField = "title"        // not clearable
	rUPC   relField = "upc"          // clearable → SQL NULL
	rGrid  relField = "grid"         // not clearable
	rDate  relField = "release_date" // NOT NULL: clearable → sentinel
	rNotes relField = "notes"        // ReadOnly
)

type relSort string

const dateSentinel = "0001-01-01"

var relFields = map[relField]Field[*release]{
	rID:    {Column: "release.id", Scan: func(r *release) any { return &r.ID }},
	rTitle: {Column: "release.title", Scan: func(r *release) any { return &r.Title }, Value: func(r *release) any { return r.Title }},
	rUPC: {Column: "release.upc", Clearable: true,
		Scan: func(r *release) any { return &r.UPC }, Value: func(r *release) any { return r.UPC }},
	rGrid: {Column: "release.grid", Scan: func(r *release) any { return &r.Grid }, Value: func(r *release) any { return r.Grid }},
	rDate: {Column: "release.release_date", Clearable: true, ClearValue: dateSentinel,
		Scan: func(r *release) any { return &r.Date }, Value: func(r *release) any { return r.Date }},
	rNotes: {Column: "COALESCE(notes.body,'')", ReadOnly: true, Join: "notes",
		Scan: func(r *release) any { return &r.Notes }},
}

func buildRelSchema(conn DataConn, extra ...Option[*release, relField, relSort, string]) *Schema[*release, relField, relSort, string] {
	base := []Option[*release, relField, relSort, string]{
		Table[*release, relField, relSort, string]("release"),
		ID[*release, relField, relSort, string](rID),
		Fields[*release, relField, relSort, string](relFields),
		OptionalJoin[*release, relField, relSort, string]("notes",
			"LEFT JOIN notes ON notes.release_id = release.id"),
		Conflict[*release, relField, relSort, string](rID),
	}
	return New(conn, append(base, extra...)...)
}

// --- criterion 1: Write/Skip/Clear(NULL)/Clear(sentinel) land in SQL --------

func TestSetRules_RulesLandInSQL(t *testing.T) {
	t.Parallel()

	conn := newConn()
	s := buildRelSchema(conn)

	err := s.DAO().With(rID, "1").
		Set(rGrid, "G1"). // staged, then removed by the Skip rule below
		SetRules(map[relField]Rule{
			rTitle: Write("New Title"),
			rUPC:   Clear(), // clearable → SQL NULL
			rDate:  Clear(), // clearable NOT NULL → sentinel
			rGrid:  Skip(),  // authoritative over the staged Set
		}).
		Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantSQL := `UPDATE "release" SET "release_date" = $1, "title" = $2, "upc" = $3 WHERE release.id = $4`
	if conn.lastExec != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastExec, wantSQL)
	}
	wantArgs := []any{dateSentinel, "New Title", nil, "1"}
	if !reflect.DeepEqual(conn.lastEArgs, wantArgs) {
		t.Errorf("args = %#v, want %#v", conn.lastEArgs, wantArgs)
	}
}

// --- criterion 2: order independence / rules are authoritative --------------

func TestSetRules_OrderIndependence(t *testing.T) {
	t.Parallel()

	rules := map[relField]Rule{rTitle: Write("ruled")}

	conn1 := newConn()
	s1 := buildRelSchema(conn1)
	if err := s1.DAO().With(rID, "1").Set(rTitle, "staged").SetRules(rules).Update(); err != nil {
		t.Fatalf("Set-then-SetRules: %v", err)
	}
	conn2 := newConn()
	s2 := buildRelSchema(conn2)
	if err := s2.DAO().With(rID, "1").SetRules(rules).Set(rTitle, "staged").Update(); err != nil {
		t.Fatalf("SetRules-then-Set: %v", err)
	}
	if conn1.lastExec != conn2.lastExec || !reflect.DeepEqual(conn1.lastEArgs, conn2.lastEArgs) {
		t.Errorf("order-dependent: %q %#v vs %q %#v",
			conn1.lastExec, conn1.lastEArgs, conn2.lastExec, conn2.lastEArgs)
	}
	if want := []any{"ruled", "1"}; !reflect.DeepEqual(conn1.lastEArgs, want) {
		t.Errorf("rule did not win: args = %#v, want %#v", conn1.lastEArgs, want)
	}
}

func TestSetRules_OverridesDefaultValues(t *testing.T) {
	t.Parallel()

	conn := newConn()
	s := buildRelSchema(conn,
		DefaultValues[*release, relField, relSort, string](map[relField]any{rTitle: "dflt"}))

	err := s.DAO().With(rID, "1").
		Set(rUPC, "u").
		SetRules(map[relField]Rule{rTitle: Skip()}).
		Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if strings.Contains(conn.lastExec, "title") {
		t.Errorf("Skip did not remove the DefaultValues entry: %q", conn.lastExec)
	}
}

func TestSetRules_LastRuleWinsPerField(t *testing.T) {
	t.Parallel()

	conn := newConn()
	s := buildRelSchema(conn)

	err := s.DAO().With(rID, "1").
		SetRules(map[relField]Rule{rTitle: Write("first")}).
		SetRules(map[relField]Rule{rTitle: Write("second")}).
		Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if want := []any{"second", "1"}; !reflect.DeepEqual(conn.lastEArgs, want) {
		t.Errorf("args = %#v, want %#v", conn.lastEArgs, want)
	}
}

// --- criterion 3: downgrade-to-skip on non-clearable fields -----------------

func TestSetRules_ClearDowngradesToSkip(t *testing.T) {
	t.Parallel()

	conn := newConn()
	s := buildRelSchema(conn)

	err := s.DAO().With(rID, "1").
		Set(rUPC, "u").
		SetRules(map[relField]Rule{rTitle: Clear()}). // not clearable → dropped
		Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if strings.Contains(conn.lastExec, "title") {
		t.Errorf("non-clearable Clear reached SQL: %q", conn.lastExec)
	}

	// All rules downgraded → empty staged set → no-op, no statement.
	conn2 := newConn()
	s2 := buildRelSchema(conn2)
	if err := s2.DAO().With(rID, "1").
		SetRules(map[relField]Rule{rTitle: Clear()}).
		Update(); err != nil {
		t.Fatalf("all-downgraded Update: %v", err)
	}
	if conn2.lastExec != "" {
		t.Errorf("all-downgraded update executed: %q", conn2.lastExec)
	}
}

// --- criterion 4: strict mode, last-rule-wins --------------------------------

func TestSetRules_StrictClears(t *testing.T) {
	t.Parallel()

	build := func(conn DataConn) *Schema[*release, relField, relSort, string] {
		return buildRelSchema(conn, StrictClears[*release, relField, relSort, string]())
	}

	// Final Clear on a non-clearable field errors; nothing executes.
	conn := newConn()
	err := build(conn).DAO().With(rID, "1").
		SetRules(map[relField]Rule{rTitle: Clear()}).
		Update()
	if !errors.Is(err, ErrNotClearable) {
		t.Fatalf("err = %v, want ErrNotClearable", err)
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("error does not name the field: %v", err)
	}
	if conn.lastExec != "" || conn.lastQuery != "" {
		t.Errorf("strict violation executed a statement: %q %q", conn.lastExec, conn.lastQuery)
	}

	// Replacement case: Clear then Skip → no error, nothing executes.
	conn2 := newConn()
	if err := build(conn2).DAO().With(rID, "1").
		SetRules(map[relField]Rule{rTitle: Clear()}).
		SetRules(map[relField]Rule{rTitle: Skip()}).
		Update(); err != nil {
		t.Fatalf("Clear→Skip: %v", err)
	}
	if conn2.lastExec != "" {
		t.Errorf("Clear→Skip executed: %q", conn2.lastExec)
	}

	// Replacement case: Clear then Write(v) → writes v.
	conn3 := newConn()
	if err := build(conn3).DAO().With(rID, "1").
		SetRules(map[relField]Rule{rTitle: Clear()}).
		SetRules(map[relField]Rule{rTitle: Write("v")}).
		Update(); err != nil {
		t.Fatalf("Clear→Write: %v", err)
	}
	if want := []any{"v", "1"}; !reflect.DeepEqual(conn3.lastEArgs, want) {
		t.Errorf("Clear→Write args = %#v, want %#v", conn3.lastEArgs, want)
	}

	// A valid clear on a clearable field is untouched by strict mode.
	conn4 := newConn()
	if err := build(conn4).DAO().With(rID, "1").
		SetRules(map[relField]Rule{rUPC: Clear()}).
		Update(); err != nil {
		t.Fatalf("strict valid clear: %v", err)
	}
	if want := []any{nil, "1"}; !reflect.DeepEqual(conn4.lastEArgs, want) {
		t.Errorf("strict valid clear args = %#v, want %#v", conn4.lastEArgs, want)
	}
}

// --- criterion 5: lenient SetRules keys vs loud Set/SetMap -------------------

func TestSetRules_LenientKeys(t *testing.T) {
	t.Parallel()

	conn := newConn()
	s := buildRelSchema(conn)

	err := s.DAO().With(rID, "1").
		SetRules(map[relField]Rule{
			"no_such_field": Write("x"), // unknown: skipped
			rNotes:          Write("y"), // ReadOnly: skipped
			rTitle:          Write("ok"),
		}).
		Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantSQL := `UPDATE "release" SET "title" = $1 WHERE release.id = $2`
	if conn.lastExec != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastExec, wantSQL)
	}

	// The developer surface stays loud.
	if err := buildRelSchema(newConn()).DAO().With(rID, "1").
		Set("no_such_field", "x").Update(); !errors.Is(err, ErrUnknownField) {
		t.Errorf("Set unknown: err = %v, want ErrUnknownField", err)
	}
	if err := buildRelSchema(newConn()).DAO().With(rID, "1").
		SetMap(map[relField]any{rNotes: "y"}).Update(); !errors.Is(err, ErrReadOnlyField) {
		t.Errorf("SetMap read-only: err = %v, want ErrReadOnlyField", err)
	}
}

// --- criterion 6: dao.New rejects ClearValue without Clearable ---------------

func TestNew_ClearValueRequiresClearable(t *testing.T) {
	t.Parallel()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("dao.New did not panic")
		}
		if msg := panicText(r); !strings.Contains(msg, "ClearValue without Clearable") ||
			!strings.Contains(msg, "title") {
			t.Errorf("panic = %v, want message naming field %q", r, "title")
		}
	}()

	bad := map[relField]Field[*release]{
		rID:    {Column: "release.id", Scan: func(r *release) any { return &r.ID }},
		rTitle: {Column: "release.title", ClearValue: "oops", Scan: func(r *release) any { return &r.Title }},
	}
	New(newConn(),
		Table[*release, relField, relSort, string]("release"),
		Fields[*release, relField, relSort, string](bad),
	)
}

// --- criterion 7: fluent Clear() honors a declared ClearValue ----------------

func TestClear_HonorsClearValue(t *testing.T) {
	t.Parallel()

	conn := newConn()
	s := buildRelSchema(conn)

	// Declared sentinel → staged; clearable-to-NULL and undeclared → nil,
	// byte-identical to the pre-ADR Set(field, nil) behavior.
	err := s.DAO().With(rID, "1").
		Clear(rDate).  // ClearValue declared → sentinel
		Clear(rUPC).   // Clearable, no ClearValue → NULL
		Clear(rTitle). // no declaration → NULL (trusted developer intent)
		Update()
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantSQL := `UPDATE "release" SET "release_date" = $1, "title" = $2, "upc" = $3 WHERE release.id = $4`
	if conn.lastExec != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastExec, wantSQL)
	}
	wantArgs := []any{dateSentinel, nil, nil, "1"}
	if !reflect.DeepEqual(conn.lastEArgs, wantArgs) {
		t.Errorf("args = %#v, want %#v", conn.lastEArgs, wantArgs)
	}
}

// --- criterion 8: Insert/Upsert honor rules ----------------------------------

func TestSetRules_InsertAndUpsert(t *testing.T) {
	t.Parallel()

	// Skip omits the column (DB default applies); Clear stages its value.
	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{"1"}}} // RETURNING id
	s := buildRelSchema(conn)
	if _, err := s.DAO().
		Set(rTitle, "t").
		Set(rGrid, "g").
		SetRules(map[relField]Rule{
			rGrid: Skip(),
			rDate: Clear(),
		}).
		Insert(); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	wantSQL := `INSERT INTO "release" ("release_date", "title") VALUES ($1, $2) RETURNING "id"`
	if conn.lastQuery != wantSQL {
		t.Errorf("insert sql = %q, want %q", conn.lastQuery, wantSQL)
	}

	// All-skip Insert is a caller bug, exactly as an empty Insert is today.
	if _, err := buildRelSchema(newConn()).DAO().
		Set(rTitle, "t").
		SetRules(map[relField]Rule{rTitle: Skip()}).
		Insert(); !errors.Is(err, ErrNothingToInsert) {
		t.Errorf("all-skip Insert: err = %v, want ErrNothingToInsert", err)
	}

	// Upsert resolves rules through the same staging.
	conn2 := newConn()
	if err := buildRelSchema(conn2).DAO().
		Set(rID, "1").
		SetRules(map[relField]Rule{rDate: Clear()}).
		Upsert(); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !strings.Contains(conn2.lastExec, `"release_date"`) ||
		!reflect.DeepEqual(conn2.lastEArgs, []any{"1", dateSentinel}) {
		t.Errorf("upsert sql/args = %q %#v", conn2.lastExec, conn2.lastEArgs)
	}
}
