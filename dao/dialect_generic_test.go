package dao

import (
	"context"
	"testing"
)

func TestGenericDialect_Basics(t *testing.T) {
	t.Parallel()

	d := GenericDialect{}
	if d.Placeholder(3) != "$3" {
		t.Errorf("Placeholder(3) = %q, want $3", d.Placeholder(3))
	}
	if d.MaxBindParams() != 65535 {
		t.Errorf("MaxBindParams() = %d, want 65535", d.MaxBindParams())
	}
	if d.MaxBatchRows() != 0 {
		t.Errorf("MaxBatchRows() = %d, want 0", d.MaxBatchRows())
	}
	if got := d.QuoteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("QuoteIdent = %q, want %q", got, `"a""b"`)
	}
}

// GenericDialect provides the SQL SHAPE and no capability at all. That is what
// makes promotion safe: a dialect embedding it cannot acquire a capability it
// never declared, because there is none to promote.
func TestGenericDialect_ImplementsNoCapability(t *testing.T) {
	t.Parallel()

	var d any = GenericDialect{}
	for _, tc := range []struct {
		name string
		is   bool
	}{
		{"Returner", func() bool { _, ok := d.(Returner); return ok }()},
		{"Copier", func() bool { _, ok := d.(Copier); return ok }()},
		{"TwoPhaser", func() bool { _, ok := d.(TwoPhaser); return ok }()},
		{"Upserter", func() bool { _, ok := d.(Upserter); return ok }()},
		{"LastInsertIDReader", func() bool { _, ok := d.(LastInsertIDReader); return ok }()},
	} {
		if tc.is {
			t.Errorf("GenericDialect implements %s; every dialect embedding it would "+
				"inherit that capability without declaring it", tc.name)
		}
	}
}

func TestGenericDialect_UpsertSuffix(t *testing.T) {
	t.Parallel()

	// The clause moved to StandardUpsertSuffix, which the dialects that can
	// upsert delegate to. Same expectations, one home.
	d := GenericDialect{}
	tests := []struct {
		name             string
		conflict, update []string
		want             string
	}{
		{"do-nothing (skip)", nil, nil, "ON CONFLICT DO NOTHING"},
		{"conflict no update", []string{"id"}, nil, `ON CONFLICT ("id") DO NOTHING`},
		{"upsert", []string{"id"}, []string{"name", "uri"},
			`ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name", "uri" = EXCLUDED."uri"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := StandardUpsertSuffix(d, tt.conflict, tt.update); got != tt.want {
				t.Errorf("BuildUpsertSuffix = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenericDialect_CopyUnsupported(t *testing.T) {
	t.Parallel()

	// GenericDialect has no Copy at all now: bulk load is a capability, and an
	// engine without it is not a Copier rather than being one that errors.
	if _, ok := any(GenericDialect{}).(Copier); ok {
		t.Error("GenericDialect satisfies Copier; it must implement no capability")
	}
}

func TestGenericDialect_TranslatePassthrough(t *testing.T) {
	t.Parallel()

	in := context.Canceled
	if out := (GenericDialect{}).TranslateError(in); out != in {
		t.Errorf("TranslateError changed the error: %v", out)
	}
}
