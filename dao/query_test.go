package dao

import (
	"reflect"
	"strings"
	"testing"
)

// compile-time: field-keyed search ops satisfy the schema-binding interface.
var (
	_ fieldSearchOp = (*stringOp)(nil)
	_ fieldSearchOp = (*exactOp)(nil)
)

// render renders a predicate against GenericDialect from placeholder $1.
func render(p Predicate) (string, []any) {
	n := 0
	return p.ToSQL(GenericDialect{}, &n)
}

func TestPredicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		p        Predicate
		wantSQL  string
		wantArgs []any
	}{
		{"eq", Eq("name", "x"), "name = $1", []any{"x"}},
		{"in", In("id", []any{1, 2, 3}), "id IN ($1, $2, $3)", []any{1, 2, 3}},
		{"in empty", In("id", nil), "1 = 0", nil},
		{"notIn", NotIn("id", []any{7}), "id NOT IN ($1)", []any{7}},
		{"notIn empty", NotIn("id", nil), "1 = 1", nil},
		{"isNull", IsNull("x"), "x IS NULL", nil},
		{"isNotNull", IsNotNull("x"), "x IS NOT NULL", nil},
		{"gt", Gt("a", 5), "a > $1", []any{5}},
		{"gte", Gte("a", 5), "a >= $1", []any{5}},
		{"lt", Lt("a", 5), "a < $1", []any{5}},
		{"lte", Lte("a", 5), "a <= $1", []any{5}},
		{"between", Between("d", 1, 9), "d BETWEEN $1 AND $2", []any{1, 9}},
		{"like", Like("n", "%a%"), "n LIKE $1", []any{"%a%"}},
		{"raw", Raw("a = ? OR b = ?", 1, 2), "a = $1 OR b = $2", []any{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sql, args := render(tt.p)
			if sql != tt.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tt.wantSQL)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("args = %#v, want %#v", args, tt.wantArgs)
			}
		})
	}
}

func TestPredicate_GroupsNumberContinuously(t *testing.T) {
	t.Parallel()

	p := And(Eq("a", 1), Or(Eq("b", 2), Eq("c", 3)), Gt("d", 4))
	sql, args := render(p)

	wantSQL := "(a = $1 AND (b = $2 OR c = $3) AND d > $4)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if !reflect.DeepEqual(args, []any{1, 2, 3, 4}) {
		t.Errorf("args = %#v, want [1 2 3 4]", args)
	}
}

func TestPredicate_EmptyAndSingleGroups(t *testing.T) {
	t.Parallel()

	if sql, _ := render(And()); sql != "1 = 1" {
		t.Errorf("And() = %q, want 1 = 1", sql)
	}
	if sql, _ := render(Or()); sql != "1 = 0" {
		t.Errorf("Or() = %q, want 1 = 0", sql)
	}
	if sql, _ := render(And(Eq("a", 1))); sql != "a = $1" {
		t.Errorf("single-member group should not parenthesize, got %q", sql)
	}
}

func TestSortsAndParse(t *testing.T) {
	t.Parallel()

	if got := Asc("name"); got != (Sort{Key: "name"}) {
		t.Errorf("Asc = %+v", got)
	}
	if got := Desc("created"); got != (Sort{Key: "created", Desc: true}) {
		t.Errorf("Desc = %+v", got)
	}
	got := ParseSorts("-created", "name", "+rank", "")
	want := []Sort{{Key: "created", Desc: true}, {Key: "name"}, {Key: "rank"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSorts = %+v, want %+v", got, want)
	}
}

func TestSearchOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		op       SearchOp
		value    string
		wantSQL  string
		wantArgs []any
	}{
		{"bool true", BoolOp("public", "artist.public"), "true", "artist.public = $1", []any{true}},
		{"bool false", BoolOp("public", "artist.public"), "no", "artist.public = $1", []any{false}},
		{"array", ArrayOp("tag", "tags"), "rock", "$1 = ANY(tags)", []any{"rock"}},
		{"string ILIKE", StringOp("name", "artist.name"), "liq", `artist.name ILIKE $1 ESCAPE '\'`, []any{"%liq%"}},
		{"exact", ExactOp("uri", "artist.uri"), "x", "artist.uri = $1", []any{"x"}},
		{"raw op", RawOp("custom", func(v string) Predicate { return Eq("c", v) }), "y", "c = $1", []any{"y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sql, args := render(tt.op.Predicate(tt.value))
			if sql != tt.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tt.wantSQL)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("args = %#v, want %#v", args, tt.wantArgs)
			}
		})
	}
}

func TestSearchOp_FieldBinding(t *testing.T) {
	t.Parallel()

	op := StringOp("name", "namefield")
	fb, ok := op.(fieldSearchOp)
	if !ok {
		t.Fatal("StringOp does not implement fieldSearchOp")
	}
	if fb.fieldKey() != "namefield" {
		t.Errorf("fieldKey() = %q, want namefield", fb.fieldKey())
	}
	// Before binding, the field key doubles as the column.
	if sql, _ := render(op.Predicate("q")); sql != `namefield ILIKE $1 ESCAPE '\'` {
		t.Errorf("unbound column = %q", sql)
	}
	// The schema binds the resolved column.
	bound := fb.withColumn("artist.name")
	if sql, _ := render(bound.Predicate("q")); sql != `artist.name ILIKE $1 ESCAPE '\'` {
		t.Errorf("bound column = %q", sql)
	}
}

func TestParseSearchQuery(t *testing.T) {
	t.Parallel()

	got := parseSearchQuery("title:liquid freetext public:true")
	want := []searchTerm{{"title", "liquid"}, {"public", "true"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseSearchQuery = %+v, want %+v", got, want)
	}
}

func TestEscapeLike(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"50%", `50\%`},
		{"a_b", `a\_b`},
		{`back\slash`, `back\\slash`},
		{`%_\`, `\%\_\\`},
	}
	for _, tc := range cases {
		if got := EscapeLike(tc.in); got != tc.want {
			t.Errorf("EscapeLike(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringOp_EscapesLikeMetacharacters(t *testing.T) {
	t.Parallel()
	op := StringOp("name", "name")
	p := op.Predicate("50%_off")
	n := 0
	sql, args := p.ToSQL(GenericDialect{}, &n)
	if !strings.Contains(sql, "ESCAPE") {
		t.Errorf("expected an explicit ESCAPE clause, got %q", sql)
	}
	if len(args) != 1 || args[0] != `%50\%\_off%` {
		t.Errorf("args = %v, want the metacharacters escaped inside the %%...%% wrap", args)
	}
}
