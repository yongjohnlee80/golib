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
	if !d.SupportsReturning() {
		t.Error("SupportsReturning() = false, want true")
	}
	if d.CopySupported() {
		t.Error("CopySupported() = true, want false")
	}
}

func TestGenericDialect_UpsertSuffix(t *testing.T) {
	t.Parallel()

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
			if got := d.BuildUpsertSuffix(tt.conflict, tt.update); got != tt.want {
				t.Errorf("BuildUpsertSuffix = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenericDialect_CopyUnsupported(t *testing.T) {
	t.Parallel()

	if _, err := (GenericDialect{}).Copy(context.Background(), nil, "t", nil, nil); err == nil {
		t.Error("Copy() error = nil, want a not-supported error")
	}
}

func TestGenericDialect_TranslatePassthrough(t *testing.T) {
	t.Parallel()

	in := context.Canceled
	if out := (GenericDialect{}).TranslateError(in); out != in {
		t.Errorf("TranslateError changed the error: %v", out)
	}
}
