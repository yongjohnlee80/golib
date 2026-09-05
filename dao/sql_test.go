package dao

import "testing"

func TestBuildBatchInsert(t *testing.T) {
	t.Parallel()

	b := &builder{dialect: GenericDialect{}}
	cols := []string{"name", "uri"}
	matrix := [][]any{{"a", "u1"}, {"b", "u2"}}

	got := b.buildBatchInsert("artist", cols, matrix, "")
	want := `INSERT INTO "artist" ("name", "uri") VALUES ($1, $2), ($3, $4)`
	if got != want {
		t.Errorf("sql = %q\nwant %q", got, want)
	}

	wantArgs := []any{"a", "u1", "b", "u2"}
	if len(b.args) != len(wantArgs) {
		t.Fatalf("args = %d, want %d", len(b.args), len(wantArgs))
	}
	for i, a := range wantArgs {
		if b.args[i] != a {
			t.Errorf("args[%d] = %v, want %v", i, b.args[i], a)
		}
	}
}

func TestBuildBatchInsert_Suffix(t *testing.T) {
	t.Parallel()

	d := GenericDialect{}
	b := &builder{dialect: d}
	suffix := StandardUpsertSuffix(d, []string{"id"}, []string{"name"})
	got := b.buildBatchInsert("t", []string{"id", "name"}, [][]any{{1, "x"}}, suffix)

	want := `INSERT INTO "t" ("id", "name") VALUES ($1, $2) ` +
		`ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name"`
	if got != want {
		t.Errorf("sql = %q\nwant %q", got, want)
	}
}

func TestBuildBatchInsert_ShortRowPadsNull(t *testing.T) {
	t.Parallel()

	b := &builder{dialect: GenericDialect{}}
	// A row shorter than cols pads the missing trailing value with nil.
	b.buildBatchInsert("t", []string{"a", "b"}, [][]any{{1}}, "")
	if len(b.args) != 2 {
		t.Fatalf("args = %d, want 2", len(b.args))
	}
	if b.args[1] != nil {
		t.Errorf("args[1] = %v, want nil", b.args[1])
	}
}
