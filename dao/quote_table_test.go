package dao

import (
	"strings"
	"testing"
)

// --- QuoteTable (ADR-0013 §2) ------------------------------------------------

func TestGenericDialect_QuoteTable(t *testing.T) {
	t.Parallel()
	d := GenericDialect{}

	// Unqualified names render byte-identically to QuoteIdent.
	if got, want := d.QuoteTable("artist"), d.QuoteIdent("artist"); got != want {
		t.Errorf("QuoteTable(artist) = %q, want %q", got, want)
	}
	// Qualified names quote each part separately.
	if got, want := d.QuoteTable("app.users"), `"app"."users"`; got != want {
		t.Errorf("QuoteTable(app.users) = %q, want %q", got, want)
	}
	// Embedded quotes still escape per part.
	if got, want := d.QuoteTable(`we"ird.ta"ble`), `"we""ird"."ta""ble"`; got != want {
		t.Errorf("QuoteTable escape = %q, want %q", got, want)
	}
}

func TestBuilder_QualifiedTablePositions(t *testing.T) {
	t.Parallel()

	sel := (&builder{dialect: GenericDialect{}}).buildSelect("app.users", []string{"users.id"}, nil, nil, nil, nil, nil)
	if want := `SELECT users.id FROM "app"."users"`; sel != want {
		t.Errorf("select = %q, want %q", sel, want)
	}

	var set orderedSet
	set.put("name", "x")
	ins := (&builder{dialect: GenericDialect{}}).buildInsert("app.users", set, "id", true)
	if want := `INSERT INTO "app"."users" ("name") VALUES ($1) RETURNING "id"`; ins != want {
		t.Errorf("insert = %q, want %q", ins, want)
	}

	var uset orderedSet
	uset.put("name", "y")
	upd := (&builder{dialect: GenericDialect{}}).buildUpdate("app.users", "id", uset, nil, []Predicate{Eq("users.id", 1)})
	if !strings.HasPrefix(upd, `UPDATE "app"."users" SET "name" = $1`) {
		t.Errorf("update = %q, want qualified UPDATE prefix", upd)
	}

	del := (&builder{dialect: GenericDialect{}}).buildDelete("app.users", "id", nil, []Predicate{Eq("users.id", 1)})
	if !strings.HasPrefix(del, `DELETE FROM "app"."users"`) {
		t.Errorf("delete = %q, want qualified DELETE prefix", del)
	}

	bat := (&builder{dialect: GenericDialect{}}).buildBatchInsert("app.users", []string{"name"}, [][]any{{"a"}}, "")
	if want := `INSERT INTO "app"."users" ("name") VALUES ($1)`; bat != want {
		t.Errorf("batch = %q, want %q", bat, want)
	}
}

// --- skip-conflicts column hint (ADR-0011 §2.3) -------------------------------

func TestBatchSuffix_SkipConflictsHintIgnoredByGeneric(t *testing.T) {
	t.Parallel()
	b := newBatchWriter[*artist, artistField](nil, GenericDialect{}, "artist")
	b.SkipConflicts()
	// The hint columns must not change the generic dialect's rendering.
	if got, want := b.suffix([]string{"name", "uri"}), "ON CONFLICT DO NOTHING"; got != want {
		t.Errorf("skip-conflicts suffix = %q, want %q", got, want)
	}
}
