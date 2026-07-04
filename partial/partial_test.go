package partial

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

type release struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	UPC   string `json:"upc"`
	Grid  string `json:"grid"`
}

// --- criterion 1: three states bind correctly (ClearOnNull) -----------------

func TestBind_ThreeStates(t *testing.T) {
	t.Parallel()

	p, err := Bind[release]([]byte(`{"title":"x","upc":null}`))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if p.State("title") != Present {
		t.Errorf("title state = %v, want Present", p.State("title"))
	}
	if p.State("upc") != Cleared {
		t.Errorf("upc state = %v, want Cleared", p.State("upc"))
	}
	if p.State("grid") != Absent || p.State("id") != Absent {
		t.Errorf("grid/id should be Absent")
	}
	d, err := p.Data()
	if err != nil {
		t.Fatalf("Data: %v", err)
	}
	if d.Title != "x" {
		t.Errorf("Data().Title = %q, want x", d.Title)
	}
	if p.Empty() {
		t.Errorf("patch should not be Empty")
	}

	empty, err := Bind[release]([]byte(`{}`))
	if err != nil {
		t.Fatalf("Bind empty: %v", err)
	}
	if !empty.Empty() {
		t.Errorf("{} should be Empty")
	}
}

// --- criterion 2: ExplicitClear compat + one clear namespace ----------------

func TestBind_ExplicitClear(t *testing.T) {
	t.Parallel()

	// null means absent in ExplicitClear mode.
	p, err := Bind[release]([]byte(`{"title":"x","upc":null}`), WithClearMode(ExplicitClear))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if p.State("upc") != Absent {
		t.Errorf("ExplicitClear: null upc = %v, want Absent", p.State("upc"))
	}

	// $clear array clears; a case variant canonicalizes.
	p2, err := Bind[release]([]byte(`{"title":"x","$clear":["UPC"]}`), WithClearMode(ExplicitClear))
	if err != nil {
		t.Fatalf("Bind $clear: %v", err)
	}
	if p2.State("upc") != Cleared {
		t.Errorf("$clear [UPC] should clear upc, got %v", p2.State("upc"))
	}

	// unknown $clear entry is ignored (never in Fields).
	p3, err := Bind[release]([]byte(`{"$clear":["nope","grid"]}`), WithClearMode(ExplicitClear))
	if err != nil {
		t.Fatalf("Bind unknown $clear: %v", err)
	}
	if got := p3.Fields(); !reflect.DeepEqual(got, []string{"grid"}) {
		t.Errorf("Fields = %v, want [grid] (unknown dropped)", got)
	}

	// non-array $clear → ValidationError naming ClearKey.
	if _, err := Bind[release]([]byte(`{"$clear":"upc"}`), WithClearMode(ExplicitClear)); !isValidationOn(err, ClearKey) {
		t.Errorf("non-array $clear: err = %v, want ValidationError on %q", err, ClearKey)
	}

	// value-AND-clear conflict via a case variant → ValidationError on title.
	if _, err := Bind[release]([]byte(`{"title":"x","$clear":["TITLE"]}`), WithClearMode(ExplicitClear)); !isValidationOn(err, "title") {
		t.Errorf("value-and-clear: err = %v, want ValidationError on title", err)
	}
}

// --- criterion 3: bind-time typed validation --------------------------------

func TestBind_TypedValidation(t *testing.T) {
	t.Parallel()

	_, err := Bind[release]([]byte(`{"title":123}`))
	if !isValidationOn(err, "title") {
		t.Fatalf("err = %v, want *ValidationError on title", err)
	}
}

// --- criterion 4: one name space, deterministic across case-fold collisions --

type tagged struct {
	Title string `json:"artist_title"`
}

func TestBind_NameSpaceAndOrder(t *testing.T) {
	t.Parallel()

	p, err := Bind[tagged]([]byte(`{"artist_title":"v"}`))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if p.State("artist_title") != Present {
		t.Errorf("presence not tracked under json name")
	}
	if p.State("ARTIST_TITLE") != Present { // case-insensitive fallback resolves to canonical
		t.Errorf("case-insensitive lookup failed")
	}

	// Last-matching-key parity, both orders, stable across repeated runs.
	for i := 0; i < 50; i++ {
		a, err := Bind[release]([]byte(`{"title":"a","TITLE":"b"}`))
		if err != nil {
			t.Fatalf("Bind ab: %v", err)
		}
		if d, _ := a.Data(); d.Title != "b" {
			t.Fatalf("title/TITLE order: got %q, want b", d.Title)
		}
		z, err := Bind[release]([]byte(`{"TITLE":"b","title":"a"}`))
		if err != nil {
			t.Fatalf("Bind ba: %v", err)
		}
		if d, _ := z.Data(); d.Title != "a" {
			t.Fatalf("TITLE/title order: got %q, want a", d.Title)
		}
	}

	// value-then-null ends Cleared; null-then-value ends Present.
	vn, _ := Bind[release]([]byte(`{"title":"x","title":null}`))
	if vn.State("title") != Cleared {
		t.Errorf("value-then-null: got %v, want Cleared", vn.State("title"))
	}
	nv, _ := Bind[release]([]byte(`{"title":null,"title":"x"}`))
	if nv.State("title") != Present {
		t.Errorf("null-then-value: got %v, want Present", nv.State("title"))
	}
}

// --- criterion 5: plan hardening --------------------------------------------

type inner struct {
	A string `json:"a"`
}
type valueEmbed struct {
	inner
	B string `json:"b"`
}
type ptrEmbed struct {
	*inner
	B string `json:"b"`
}
type clearKeyField struct {
	Bad string `json:"$clear"`
}

func TestPlan_Hardening(t *testing.T) {
	t.Parallel()

	// value embed flattens.
	p, err := Bind[valueEmbed]([]byte(`{"a":"x","b":"y"}`))
	if err != nil {
		t.Fatalf("Bind value embed: %v", err)
	}
	if !p.Contains("a", "b") {
		t.Errorf("value embed fields not flattened: %v", p.Fields())
	}

	// pointer embed panics at plan build, naming type + field.
	assertPanics(t, "ptr embed", "anonymous pointer embed", func() { planFor[ptrEmbed]() })
	// a canonical "$clear" field panics.
	assertPanics(t, "clearkey field", ClearKey, func() { planFor[clearKeyField]() })

	// plan is cached: same pointer across calls.
	if planFor[release]() != planFor[release]() {
		t.Errorf("planFor not cached")
	}
}

// --- criterion 6: mutator semantics -----------------------------------------

func TestMutators(t *testing.T) {
	t.Parallel()

	// Set drops a pending clear (last-mutation-wins-over-clear).
	p, _ := Bind[release]([]byte(`{"upc":null}`))
	p.Set("upc", "123")
	if p.State("upc") != Present {
		t.Errorf("Set after clear: got %v, want Present", p.State("upc"))
	}
	if d, _ := p.Data(); d.UPC != "123" {
		t.Errorf("Set value not applied: %q", d.UPC)
	}

	// Clear drops a value; Remove strips both; Only intersects.
	p2, _ := Bind[release]([]byte(`{"title":"t","upc":"u"}`))
	p2.Clear("title")
	if p2.State("title") != Cleared {
		t.Errorf("Clear: got %v", p2.State("title"))
	}
	p2.Remove("upc")
	if p2.State("upc") != Absent {
		t.Errorf("Remove: got %v", p2.State("upc"))
	}

	p3, _ := Bind[release]([]byte(`{"title":"t","upc":"u","grid":"g"}`))
	p3.Only("title")
	if got := p3.Fields(); !reflect.DeepEqual(got, []string{"title"}) {
		t.Errorf("Only: %v, want [title]", got)
	}

	// Cache invalidation: Data reflects a mutation.
	p4, _ := Bind[release]([]byte(`{"title":"a"}`))
	_, _ = p4.Data()
	p4.Set("title", "b")
	if d, _ := p4.Data(); d.Title != "b" {
		t.Errorf("cache not invalidated: %q", d.Title)
	}

	// Unknown mutator field is loud; valid mutations before it still apply.
	p5, _ := Bind[release]([]byte(`{}`))
	p5.Set("title", "ok").Set("nope", "x")
	if !errors.Is(p5.Err(), ErrUnknownField) {
		t.Errorf("unknown Set: Err = %v, want ErrUnknownField", p5.Err())
	}
	if _, err := p5.Rules(); !errors.Is(err, ErrUnknownField) {
		t.Errorf("Rules should surface sticky err")
	}
}

// --- criterion 7: rules projection + ApplyRules onto a dao fake -------------

func TestRules_And_ApplyRules(t *testing.T) {
	t.Parallel()

	p, _ := Bind[release]([]byte(`{"title":"x","upc":null}`))
	p.Set("grid", "g")

	rules, err := p.Rules()
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	want := map[string]Rule{
		"title": {Kind: RuleWrite, Value: "x"},
		"grid":  {Kind: RuleWrite, Value: "g"},
		"upc":   {Kind: RuleClear},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Errorf("Rules = %#v, want %#v", rules, want)
	}

	// ApplyRules onto a dao fake: UPDATE writes title+grid, clears upc.
	conn := newFakeConn()
	s := buildReleaseSchema(conn)
	d, err := ApplyRules(s.DAO().With(relID, "1"), p)
	if err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
	if err := d.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantSQL := `UPDATE "release" SET "grid" = $1, "title" = $2, "upc" = $3 WHERE release.id = $4`
	if conn.lastExec != wantSQL {
		t.Errorf("sql = %q, want %q", conn.lastExec, wantSQL)
	}
	wantArgs := []any{"g", "x", nil, "1"}
	if !reflect.DeepEqual(conn.lastEArgs, wantArgs) {
		t.Errorf("args = %#v, want %#v", conn.lastEArgs, wantArgs)
	}
}

// --- criterion 8: composition & round-trip ----------------------------------

type outer struct {
	Name  string         `json:"name"`
	Inner Patch[release] `json:"inner"`
}

func TestComposition_And_RoundTrip(t *testing.T) {
	t.Parallel()

	o, err := Bind[outer]([]byte(`{"name":"n","inner":{"title":"t","upc":null}}`))
	if err != nil {
		t.Fatalf("Bind outer: %v", err)
	}
	if o.State("inner") != Present {
		t.Fatalf("outer inner not Present")
	}
	d, err := o.Data()
	if err != nil {
		t.Fatalf("Data: %v", err)
	}
	if d.Inner.State("title") != Present || d.Inner.State("upc") != Cleared {
		t.Errorf("inner patch states wrong: title=%v upc=%v",
			d.Inner.State("title"), d.Inner.State("upc"))
	}

	// Round-trip both modes: marshal → Bind → same three states.
	for _, mode := range []ClearMode{ClearOnNull, ExplicitClear} {
		src, _ := Bind[release]([]byte(`{"title":"t","upc":null}`), WithClearMode(mode))
		if mode == ExplicitClear {
			// null is absent in ExplicitClear; re-add upc as an explicit clear.
			src.Clear("upc")
		}
		raw, err := src.MarshalJSON()
		if err != nil {
			t.Fatalf("Marshal(%v): %v", mode, err)
		}
		back, err := Bind[release](raw, WithClearMode(mode))
		if err != nil {
			t.Fatalf("re-Bind(%v): %v: %s", mode, err, raw)
		}
		if back.State("title") != Present || back.State("upc") != Cleared {
			t.Errorf("mode %v round-trip lost state: title=%v upc=%v (%s)",
				mode, back.State("title"), back.State("upc"), raw)
		}
	}
}

// --- helpers ----------------------------------------------------------------

func isValidationOn(err error, field string) bool {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return false
	}
	for _, f := range ve.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}

func assertPanics(t *testing.T, what, substr string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("%s: expected panic", what)
			return
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, substr) {
			t.Errorf("%s: panic = %v, want containing %q", what, r, substr)
		}
	}()
	fn()
}

// --- dao fake (mirrors dao/engine_test.go's fakeConn shape) -----------------

type relField string

const (
	relID    relField = "id"
	relTitle relField = "title"
	relUPC   relField = "upc"
	relGrid  relField = "grid"
)

type relSort string

func buildReleaseSchema(conn dao.DataConn) *dao.Schema[*release, relField, relSort, string] {
	fields := map[relField]dao.Field[*release]{
		relID:    {Column: "release.id", Scan: func(r *release) any { return &r.ID }},
		relTitle: {Column: "release.title", Clearable: true, Scan: func(r *release) any { return &r.Title }, Value: func(r *release) any { return r.Title }},
		relUPC:   {Column: "release.upc", Clearable: true, Scan: func(r *release) any { return &r.UPC }, Value: func(r *release) any { return r.UPC }},
		relGrid:  {Column: "release.grid", Clearable: true, Scan: func(r *release) any { return &r.Grid }, Value: func(r *release) any { return r.Grid }},
	}
	return dao.New(conn,
		dao.Table[*release, relField, relSort, string]("release"),
		dao.ID[*release, relField, relSort, string](relID),
		dao.Fields[*release, relField, relSort, string](fields),
	)
}

type fakeConn struct {
	d         dao.Dialect
	lastExec  string
	lastEArgs []any
}

func newFakeConn() *fakeConn { return &fakeConn{d: dao.GenericDialect{}} }

func (c *fakeConn) QueryContext(_ context.Context, q string, args ...any) (dao.Rows, error) {
	return &fakeRows{}, nil
}
func (c *fakeConn) ExecContext(_ context.Context, q string, args ...any) (dao.Result, error) {
	c.lastExec, c.lastEArgs = q, args
	return fakeResult{}, nil
}
func (c *fakeConn) Dialect() dao.Dialect                      { return c.d }
func (c *fakeConn) Begin(context.Context) (dao.TxConn, error) { return nil, errors.New("no tx") }
func (c *fakeConn) Name() string                              { return "fake" }
func (c *fakeConn) Close() error                              { return nil }

type fakeRows struct{}

func (r *fakeRows) Next() bool        { return false }
func (r *fakeRows) Scan(...any) error { return nil }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Err() error        { return nil }

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }
