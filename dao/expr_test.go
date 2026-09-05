package dao

import (
	"strings"
	"testing"
)

// mysqlLike quotes with backticks and implements TableQuoter, so a qualified
// name splits per part (the MySQL shape).
type mysqlLike struct{ GenericDialect }

func (mysqlLike) QuoteIdent(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "") + "`"
}

func (d mysqlLike) QuoteTable(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = d.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

// --- criterion 1: rendering + quoting ---------------------------------------

func TestExpr_TAndCRenderPerDialect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		d       Dialect
		wantT   string
		wantC   string
		wantQal string // T over a schema-qualified table constant
	}{
		// GenericDialect does not implement TableQuoter: the whole table string
		// quotes as ONE identifier (the ADR-0013 fallback).
		{"generic", GenericDialect{}, `"artist"."name"`, `"name"`, `"app.users"."name"`},
		{"tableQuoter", tqDialect{}, `"artist"."name"`, `"name"`, `"app"."users"."name"`},
		{"mysqlLike", mysqlLike{}, "`artist`.`name`", "`name`", "`app`.`users`.`name`"},
		// A lagging embedder with its own QuoteIdent and no TableQuoter must be
		// rendered with ITS conventions (the dao-m1 must-fix #1 shape).
		{"bqLike", bqLike{}, "`artist`.`name`", "`name`", "`app.users`.`name`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := T("artist", aName).render(c.d); got != c.wantT {
				t.Errorf("T = %s, want %s", got, c.wantT)
			}
			if got := C(aName).render(c.d); got != c.wantC {
				t.Errorf("C = %s, want %s", got, c.wantC)
			}
			if got := T("app.users", aName).render(c.d); got != c.wantQal {
				t.Errorf("T(qualified) = %s, want %s", got, c.wantQal)
			}
		})
	}

	// Generic over ~string: a typed field enum and an untyped constant both
	// compile without conversion, and the write identity is the raw column.
	const tbl = "artist"
	if e := T(tbl, aName); e.write != "name" {
		t.Errorf("T write identity = %q, want %q", e.write, "name")
	}
	if e := C(aURI); e.write != "uri" {
		t.Errorf("C write identity = %q, want %q", e.write, "uri")
	}
}

// --- criteria 2 + 3b: read AND write equivalence, no map mutation -----------

// exprFields mirrors artistFields exactly, declared with Exprs.
func exprFields() map[artistField]Field[*artist] {
	return map[artistField]Field[*artist]{
		aID:   {Expr: T("artist", aID), Scan: func(a *artist) any { return &a.ID }},
		aName: {Expr: T("artist", aName), Scan: func(a *artist) any { return &a.Name }, Value: func(a *artist) any { return a.Name }},
		aURI:  {Expr: T("artist", aURI), Scan: func(a *artist) any { return &a.URI }, Value: func(a *artist) any { return a.URI }},
		aLabelGroup: {Expr: Coalesce(T("label_group", "name"), ""), Join: "label_group", ReadOnly: true,
			Scan: func(a *artist) any { return &a.LabelGroup }},
		aPublic: {Expr: T("artist", aPublic), Scan: func(a *artist) any { return &a.Public }, Value: func(a *artist) any { return a.Public }},
	}
}

// stringFieldsQuoted is the hand-written equivalent of exprFields on a
// GenericDialect: exactly what an author would type today.
func stringFieldsQuoted() map[artistField]Field[*artist] {
	return map[artistField]Field[*artist]{
		aID:   {Column: `"artist"."id"`, WriteColumn: "id", Scan: func(a *artist) any { return &a.ID }},
		aName: {Column: `"artist"."name"`, WriteColumn: "name", Scan: func(a *artist) any { return &a.Name }, Value: func(a *artist) any { return a.Name }},
		aURI:  {Column: `"artist"."uri"`, WriteColumn: "uri", Scan: func(a *artist) any { return &a.URI }, Value: func(a *artist) any { return a.URI }},
		aLabelGroup: {Column: `COALESCE("label_group"."name", '')`, Join: "label_group", ReadOnly: true,
			Scan: func(a *artist) any { return &a.LabelGroup }},
		aPublic: {Column: `"artist"."public"`, WriteColumn: "public", Scan: func(a *artist) any { return &a.Public }, Value: func(a *artist) any { return a.Public }},
	}
}

func schemaWith(conn DataConn, fields map[artistField]Field[*artist]) *Schema[*artist, artistField, artistSort, string] {
	return New(conn,
		Table[*artist, artistField, artistSort, string]("artist"),
		ID[*artist, artistField, artistSort, string](aID),
		Fields[*artist, artistField, artistSort, string](fields),
		Default[*artist, artistField, artistSort, string](aID, aName, aURI),
		OptionalJoin[*artist, artistField, artistSort, string]("label_group",
			"LEFT JOIN label_group ON label_group.id = artist.label_group_id"),
		Conflict[*artist, artistField, artistSort, string](aURI),
	)
}

func TestExpr_ReadAndWriteEquivalence(t *testing.T) {
	t.Parallel()

	type emitted struct{ sel, ins, upd, ups string }
	run := func(fields map[artistField]Field[*artist]) emitted {
		var e emitted
		c1 := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
		s := schemaWith(c1, fields)
		_, _ = s.DAO().With(aID, "1").Select()
		e.sel = c1.lastQuery
		c2 := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
		s2 := schemaWith(c2, fields)
		_, _ = s2.DAO().Set(aName, "x").Set(aURI, "u").Insert()
		e.ins = c2.lastQuery
		c3 := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
		s3 := schemaWith(c3, fields)
		_ = s3.DAO().With(aID, "1").Set(aName, "x").Update()
		e.upd = c3.lastExec
		c4 := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
		s4 := schemaWith(c4, fields)
		_ = s4.DAO().Set(aName, "x").Set(aURI, "u").Upsert()
		e.ups = c4.lastExec
		return e
	}

	want, got := run(stringFieldsQuoted()), run(exprFields())
	if got.sel != want.sel {
		t.Errorf("SELECT\n got %s\nwant %s", got.sel, want.sel)
	}
	if got.ins != want.ins {
		t.Errorf("INSERT\n got %s\nwant %s", got.ins, want.ins)
	}
	if got.upd != want.upd {
		t.Errorf("UPDATE\n got %s\nwant %s", got.upd, want.upd)
	}
	if got.ups != want.ups {
		t.Errorf("UPSERT\n got %s\nwant %s", got.ups, want.ups)
	}

	// The rev-0 defect, pinned: a T-declared writable column must appear ONCE
	// quoted in DML, never as a doubly-quoted identifier.
	if strings.Contains(got.ins, `""`) || strings.Contains(got.upd, `""`) {
		t.Errorf("write path double-quoted an identifier:\nINSERT %s\nUPDATE %s", got.ins, got.upd)
	}
	if !strings.Contains(got.ins, `"name"`) {
		t.Errorf("INSERT should name the raw write column once quoted: %s", got.ins)
	}
}

func TestExpr_DeclarationMapNeverMutated(t *testing.T) {
	t.Parallel()

	shared := exprFields() // stands in for a package-level var

	// Two schemas, two dialects, one declaration map.
	cPg := &fakeConn{d: tqDialect{}, rows: &fakeRows{}}
	cMy := &fakeConn{d: mysqlLike{}, rows: &fakeRows{}}
	sPg, sMy := schemaWith(cPg, shared), schemaWith(cMy, shared)

	_, _ = sPg.DAO().Select(aName)
	_, _ = sMy.DAO().Select(aName)
	if !strings.Contains(cPg.lastQuery, `"artist"."name"`) {
		t.Errorf("postgres-shaped dialect: %s", cPg.lastQuery)
	}
	if !strings.Contains(cMy.lastQuery, "`artist`.`name`") {
		t.Errorf("mysql-shaped dialect: %s", cMy.lastQuery)
	}

	// The source map must be untouched: still empty Column, still non-nil Expr.
	for key, f := range shared {
		if f.Column != "" {
			t.Errorf("field %q: source map was mutated (Column = %q)", key, f.Column)
		}
		if !f.Expr.isSet() {
			t.Errorf("field %q: source map lost its Expr", key)
		}
		if f.WriteColumn != "" {
			t.Errorf("field %q: source map gained WriteColumn %q", key, f.WriteColumn)
		}
	}

	// And a third New over the same map must still work (it would panic with
	// "sets both Column and Expr" if resolution had written through).
	c3 := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
	_ = schemaWith(c3, shared)
}

// --- criterion 3: both-set is a declaration error ---------------------------

func TestExpr_BothColumnAndExprPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "sets both Column and Expr") || !strings.Contains(msg, "name") {
			t.Errorf("panic = %v, want one naming the field and both-set", r)
		}
	}()
	f := exprFields()
	f[aName] = Field[*artist]{Column: "artist.name", Expr: T("artist", aName)}
	_ = schemaWith(&fakeConn{d: returningDialect{}, rows: &fakeRows{}}, f)
}

// --- criteria 4 + 5: literals, the closed set, and refusals ----------------

func TestExpr_CoalesceAndLiterals(t *testing.T) {
	t.Parallel()

	d := GenericDialect{}
	col := T("t", "c")
	cases := []struct{ got, want string }{
		{Coalesce(col, "").render(d), `COALESCE("t"."c", '')`},
		{Coalesce(col, Str("")).render(d), `COALESCE("t"."c", '')`},
		{Coalesce(col, SQL("''")).render(d), `COALESCE("t"."c", '')`},
		{Coalesce(col, 0).render(d), `COALESCE("t"."c", 0)`},
		{Coalesce(col, int64(0)).render(d), `COALESCE("t"."c", 0)`},
		{Coalesce(col, Int(0)).render(d), `COALESCE("t"."c", 0)`},
		{Coalesce(col, "n/a").render(d), `COALESCE("t"."c", 'n/a')`},
		{Coalesce(col, -7).render(d), `COALESCE("t"."c", -7)`},
		{SQL("NOW()").render(d), "NOW()"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %s, want %s", c.got, c.want)
		}
	}

	// A Coalesce carries no write identity.
	if e := Coalesce(col, ""); e.write != "" {
		t.Errorf("Coalesce write identity = %q, want empty", e.write)
	}
}

// TestExpr_AltTermsAllRoute is the lockstep invariant: every term of Alt must
// produce a non-nil renderer, so widening Alt without adding a lit case fails
// here instead of nil-dereferencing at statement time.
func TestExpr_AltTermsAllRoute(t *testing.T) {
	t.Parallel()

	if e := lit(Str("x")); !e.isSet() {
		t.Error("Alt term Expr did not route")
	}
	if e := lit("x"); !e.isSet() {
		t.Error("Alt term string did not route")
	}
	if e := lit(1); !e.isSet() {
		t.Error("Alt term int did not route")
	}
	if e := lit(int64(1)); !e.isSet() {
		t.Error("Alt term int64 did not route")
	}
}

func TestExpr_StrRefusesUnportableInput(t *testing.T) {
	t.Parallel()

	for name, s := range map[string]string{
		"single quote": "it's",
		"backslash":    `a\b`,
		"newline":      "a\nb",
		"tab":          "a\tb",
		"del":          "a\x7fb",
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Str(%q) must panic", s)
				}
			}()
			_ = Str(s)
		})
	}

	// Coalesce's string sugar routes through Str, so it inherits the refusal.
	t.Run("via Coalesce", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Coalesce with an unportable string must panic")
			}
		}()
		_ = Coalesce(T("t", "c"), "it's")
	})
}

func TestExpr_ZeroExprPanicsAtDeclaration(t *testing.T) {
	t.Parallel()

	for name, fn := range map[string]func(){
		"Coalesce base": func() { _ = Coalesce(Expr{}, "") },
		"Coalesce alt":  func() { _ = Coalesce(T("t", "c"), Expr{}) },
		"LeftJoin left": func() { _ = LeftJoin("t", Expr{}, T("t", "c")) },
		"LeftJoin right": func() {
			_ = LeftJoin("t", T("t", "c"), Expr{})
		},
		"InnerJoin": func() { _ = InnerJoin("t", Expr{}, Expr{}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected a panic on a zero Expr")
				}
			}()
			fn()
		})
	}
}

// --- criterion 6: join clause rendering ------------------------------------

func TestExpr_JoinHelpers(t *testing.T) {
	t.Parallel()

	e := LeftJoin("label_group", T("label_group", "id"), T("artist", "label_group_id"))
	if got, want := e.render(GenericDialect{}),
		`LEFT JOIN "label_group" ON "label_group"."id" = "artist"."label_group_id"`; got != want {
		t.Errorf("LeftJoin\n got %s\nwant %s", got, want)
	}
	if got, want := InnerJoin("lg", C("a"), C("b")).render(GenericDialect{}),
		`INNER JOIN "lg" ON "a" = "b"`; got != want {
		t.Errorf("InnerJoin\n got %s\nwant %s", got, want)
	}
	// Table position: a TableQuoter dialect splits, a non-implementer does not.
	q := LeftJoin("app.lg", C("a"), C("b"))
	if got, want := q.render(tqDialect{}), `LEFT JOIN "app"."lg" ON "a" = "b"`; got != want {
		t.Errorf("qualified join (TableQuoter)\n got %s\nwant %s", got, want)
	}
	if got, want := q.render(GenericDialect{}), `LEFT JOIN "app.lg" ON "a" = "b"`; got != want {
		t.Errorf("qualified join (fallback)\n got %s\nwant %s", got, want)
	}
}

// --- criterion 7: OptionalJoinExpr + same-key precedence -------------------

func TestExpr_OptionalJoinExprAndPrecedence(t *testing.T) {
	t.Parallel()

	type O = Option[*artist, artistField, artistSort, string]
	build := func(conn DataConn, joins ...O) *Schema[*artist, artistField, artistSort, string] {
		base := []O{
			Table[*artist, artistField, artistSort, string]("artist"),
			ID[*artist, artistField, artistSort, string](aID),
			Fields[*artist, artistField, artistSort, string](exprFields()),
			Default[*artist, artistField, artistSort, string](aID, aName),
		}
		return New(conn, append(base, joins...)...)
	}

	exprForm := OptionalJoinExpr[*artist, artistField, artistSort, string]("label_group",
		LeftJoin("label_group", T("label_group", "id"), T("artist", "label_group_id")))
	stringForm := OptionalJoin[*artist, artistField, artistSort, string]("label_group",
		"LEFT JOIN legacy_string_form ON 1=1")

	// The Expr form applies on exactly the same demand-driven trigger.
	c := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
	s := build(c, exprForm)
	if _, err := s.DAO().Select(aLabelGroup); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.lastQuery, `LEFT JOIN "label_group" ON "label_group"."id" = "artist"."label_group_id"`) {
		t.Errorf("OptionalJoinExpr clause missing: %s", c.lastQuery)
	}
	// Untouched: no join when nothing triggers it.
	c2 := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
	s2 := build(c2, exprForm)
	if _, err := s2.DAO().Select(aID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c2.lastQuery, "JOIN") {
		t.Errorf("join applied without a trigger: %s", c2.lastQuery)
	}

	// Precedence, both orders: the later option wins and the opposite
	// representation must be gone.
	for _, tc := range []struct {
		name  string
		opts  []O
		want  string
		avoid string
	}{
		{"string then expr", []O{stringForm, exprForm}, `"label_group"."id"`, "legacy_string_form"},
		{"expr then string", []O{exprForm, stringForm}, "legacy_string_form", `"label_group"."id"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cc := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
			ss := build(cc, tc.opts...)
			if _, err := ss.DAO().Select(aLabelGroup); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(cc.lastQuery, tc.want) {
				t.Errorf("want %s in %s", tc.want, cc.lastQuery)
			}
			if strings.Contains(cc.lastQuery, tc.avoid) {
				t.Errorf("stale opposite representation %s in %s", tc.avoid, cc.lastQuery)
			}
		})
	}
}

// --- criterion 8: write-column safety -------------------------------------

func TestExpr_WriteColumnSafety(t *testing.T) {
	t.Parallel()

	build := func(f Field[*artist]) {
		fields := exprFields()
		fields[aName] = f
		_ = schemaWith(&fakeConn{d: returningDialect{}, rows: &fakeRows{}}, fields)
	}
	mk := func(f Field[*artist]) func(*testing.T) {
		return func(*testing.T) { build(f) }
	}
	scan := func(a *artist) any { return &a.Name }

	t.Run("writable expression panics", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected a panic")
			}
			msg, _ := r.(string)
			if !strings.Contains(msg, "not a plain identifier") || !strings.Contains(msg, "name") {
				t.Errorf("panic = %v", r)
			}
		}()
		build(Field[*artist]{Expr: Coalesce(T("t", "c"), ""), Scan: scan})
	})

	// The two documented ways out both construct fine.
	t.Run("ReadOnly is fine", mk(Field[*artist]{Expr: Coalesce(T("t", "c"), ""), Scan: scan, ReadOnly: true}))
	t.Run("explicit WriteColumn is fine", mk(Field[*artist]{Expr: Coalesce(T("t", "c"), ""), Scan: scan, WriteColumn: "name"}))
	// A plain string declaration (today's form) must keep working.
	t.Run("string column is fine", mk(Field[*artist]{Column: "artist.name", Scan: scan}))
	// A quoted-but-plain Expr column is fine: T carries the raw write identity.
	t.Run("T column is fine", mk(Field[*artist]{Expr: T("artist", aName), Scan: scan}))
}

// --- criterion 10: no query-time cost -------------------------------------

func BenchmarkSelect_ExprDeclared(b *testing.B) {
	conn := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
	s := schemaWith(conn, exprFields())
	b.ReportAllocs()
	for b.Loop() {
		_, _ = s.DAO().With(aID, "1").Select()
	}
}

func BenchmarkSelect_StringDeclared(b *testing.B) {
	conn := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
	s := schemaWith(conn, stringFieldsQuoted())
	b.ReportAllocs()
	for b.Loop() {
		_, _ = s.DAO().With(aID, "1").Select()
	}
}

// TestExpr_ResolutionConsumesTheExpr pins the §2.2 decision that the resolved
// clone drops the renderer closure. It is not bookkeeping: retaining closures
// left the Expr-declared schema measurably slower per query (~4%, identical
// allocations) purely through GC pressure from the extra live objects. With the
// Expr consumed, the two declaration forms are indistinguishable.
func TestExpr_ResolutionConsumesTheExpr(t *testing.T) {
	t.Parallel()

	src := exprFields()
	s := schemaWith(&fakeConn{d: returningDialect{}, rows: &fakeRows{}}, src)
	for key, f := range s.fields {
		if f.Expr.isSet() {
			t.Errorf("field %q: schema retained its Expr closure after resolution", key)
		}
		if f.Column == "" {
			t.Errorf("field %q: resolution produced no Column", key)
		}
	}
	// The caller's map still has them — only the schema's copy is cleared.
	for key, f := range src {
		if !f.Expr.isSet() {
			t.Errorf("field %q: the source declaration lost its Expr", key)
		}
	}
}

// TestJoins_FilterIsNotATrigger pins the join-trigger matrix that Field.Join,
// README "On-demand joins" and USAGE §3 all now state explicitly: a predicate is
// NOT a trigger. It exists because the opposite claim sat in Field.Join's doc
// comment until a live Postgres run rejected the SQL it implied.
func TestJoins_FilterIsNotATrigger(t *testing.T) {
	t.Parallel()

	filterOnJoined := func(run func(DAO[*artist, artistField, string])) (string, string) {
		c := &fakeConn{d: returningDialect{}, rows: &fakeRows{}}
		s := schemaWith(c, exprFields())
		run(s.DAO())
		return c.lastQuery, c.lastExec
	}

	cases := []struct {
		name     string
		run      func(DAO[*artist, artistField, string])
		wantJoin bool
	}{
		{"Select without the joined column", func(d DAO[*artist, artistField, string]) {
			_, _ = d.With(aLabelGroup, "x").Select(aID, aName)
		}, false},
		{"Select WITH the joined column", func(d DAO[*artist, artistField, string]) {
			_, _ = d.With(aLabelGroup, "x").Select(aID, aLabelGroup)
		}, true},
		{"Count", func(d DAO[*artist, artistField, string]) {
			_, _ = d.With(aLabelGroup, "x").Count()
		}, false},
		{"Exists", func(d DAO[*artist, artistField, string]) {
			_, _ = d.With(aLabelGroup, "x").Exists()
		}, false},
		{"Update", func(d DAO[*artist, artistField, string]) {
			_ = d.With(aLabelGroup, "x").Set(aName, "n").Update()
		}, false},
		{"Delete", func(d DAO[*artist, artistField, string]) {
			_ = d.With(aLabelGroup, "x").Delete()
		}, false},
		// DAO.Join is the documented remedy for every row above.
		{"Count + forced Join", func(d DAO[*artist, artistField, string]) {
			_, _ = d.Join("label_group").With(aLabelGroup, "x").Count()
		}, true},
		{"Update + forced Join", func(d DAO[*artist, artistField, string]) {
			_ = d.Join("label_group").With(aLabelGroup, "x").Set(aName, "n").Update()
		}, true},
		{"Delete + forced Join", func(d DAO[*artist, artistField, string]) {
			_ = d.Join("label_group").With(aLabelGroup, "x").Delete()
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, e := filterOnJoined(tc.run)
			sql := q
			if e != "" {
				sql = e
			}
			if got := strings.Contains(sql, "JOIN"); got != tc.wantJoin {
				t.Errorf("JOIN emitted = %v, want %v\n  %s", got, tc.wantJoin, sql)
			}
			// The forced-join write path must use the portable id-subselect,
			// since UPDATE/DELETE cannot JOIN directly.
			if tc.wantJoin && e != "" && !strings.Contains(e, "IN (SELECT") {
				t.Errorf("forced join on a write should use the id-subselect: %s", e)
			}
		})
	}
}
