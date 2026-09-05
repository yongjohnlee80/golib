package dao

import (
	"strings"
	"testing"
)

// tqDialect opts into the TableQuoter capability (the postgres/sqlite shape).
type tqDialect struct{ GenericDialect }

func (d tqDialect) QuoteTable(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = d.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

// bqLike models a lagging embedder with its own quoting conventions (the
// BigQuery shape from lector's dao-m1 r1 must-fix #1): it overrides
// QuoteIdent to a backtick dot-path and does NOT implement TableQuoter. The
// engine's fallback must use ITS QuoteIdent — the capability design exists
// precisely so no promoted default can override it.
type bqLike struct{ GenericDialect }

func (bqLike) QuoteIdent(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "") + "`"
}

// --- quoteTable probe + fallback (ADR-0013 §2, rev 1) -------------------------

func TestQuoteTable_FallbackKeepsHistoricalBehavior(t *testing.T) {
	t.Parallel()
	// GenericDialect does not implement TableQuoter: the whole string quotes
	// as ONE identifier — byte-identical to pre-ADR-0013 output.
	if got, want := quoteTable(GenericDialect{}, "app.users"), `"app.users"`; got != want {
		t.Errorf("fallback = %q, want %q", got, want)
	}
	if _, ok := any(GenericDialect{}).(TableQuoter); ok {
		t.Fatal("GenericDialect must NOT implement TableQuoter (promotion hazard, ADR-0013 §2)")
	}
}

func TestQuoteTable_NonImplementerKeepsOwnQuoteIdent(t *testing.T) {
	t.Parallel()
	// The BigQuery regression pin: an embedder with custom QuoteIdent and no
	// TableQuoter keeps its own conventions in table position.
	if got, want := quoteTable(bqLike{}, "dataset.table"), "`dataset.table`"; got != want {
		t.Errorf("bq-like table quote = %q, want %q", got, want)
	}
}

func TestQuoteTable_OptInSplitsQualifiedNames(t *testing.T) {
	t.Parallel()
	d := tqDialect{}
	if got, want := quoteTable(d, "app.users"), `"app"."users"`; got != want {
		t.Errorf("opt-in = %q, want %q", got, want)
	}
	// Unqualified names render identically to QuoteIdent.
	if got, want := quoteTable(d, "artist"), d.QuoteIdent("artist"); got != want {
		t.Errorf("opt-in unqualified = %q, want %q", got, want)
	}
}

func TestBuilder_QualifiedTablePositions(t *testing.T) {
	t.Parallel()

	sel := (&builder{dialect: tqDialect{}}).buildSelect("app.users", []string{"users.id"}, nil, nil, nil, nil, nil)
	if want := `SELECT users.id FROM "app"."users"`; sel != want {
		t.Errorf("select = %q, want %q", sel, want)
	}

	var set orderedSet
	set.put("name", "x")
	ins := (&builder{dialect: tqDialect{}}).buildInsert("app.users", set, "id", true)
	if want := `INSERT INTO "app"."users" ("name") VALUES ($1) RETURNING "id"`; ins != want {
		t.Errorf("insert = %q, want %q", ins, want)
	}

	var uset orderedSet
	uset.put("name", "y")
	upd := (&builder{dialect: tqDialect{}}).buildUpdate("app.users", "id", uset, nil, []Predicate{Eq("users.id", 1)})
	if !strings.HasPrefix(upd, `UPDATE "app"."users" SET "name" = $1`) {
		t.Errorf("update = %q, want qualified UPDATE prefix", upd)
	}

	del := (&builder{dialect: tqDialect{}}).buildDelete("app.users", "id", nil, []Predicate{Eq("users.id", 1)})
	if !strings.HasPrefix(del, `DELETE FROM "app"."users"`) {
		t.Errorf("delete = %q, want qualified DELETE prefix", del)
	}

	bat := (&builder{dialect: tqDialect{}}).buildBatchInsert("app.users", []string{"name"}, [][]any{{"a"}}, "")
	if want := `INSERT INTO "app"."users" ("name") VALUES ($1)`; bat != want {
		t.Errorf("batch = %q, want %q", bat, want)
	}

	// A non-implementing dialect keeps the historical single-identifier form.
	old := (&builder{dialect: GenericDialect{}}).buildSelect("app.users", []string{"users.id"}, nil, nil, nil, nil, nil)
	if want := `SELECT users.id FROM "app.users"`; old != want {
		t.Errorf("fallback select = %q, want %q", old, want)
	}
}

// --- skip-conflicts column hint (ADR-0011 §2.3) -------------------------------

func TestBatchSuffix_SkipConflictsHintIgnoredByGeneric(t *testing.T) {
	t.Parallel()
	b := newBatchWriter[*artist, artistField](nil, returningDialect{}, "artist")
	b.SkipConflicts()
	// The hint columns must not change the dialect's rendering.
	if got, want := b.suffix([]string{"name", "uri"}), "ON CONFLICT DO NOTHING"; got != want {
		t.Errorf("skip-conflicts suffix = %q, want %q", got, want)
	}
}
