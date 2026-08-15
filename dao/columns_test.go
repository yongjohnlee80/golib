package dao

import (
	"errors"
	"testing"
)

// colRows is a fakeRows that also reports column names (dao.RowsColumns).
type colRows struct {
	fakeRows
	cols []string
}

func (r *colRows) Columns() ([]string, error) { return r.cols, nil }

func TestColumns_ProbesOptionalInterface(t *testing.T) {
	t.Parallel()

	// A driver exposing RowsColumns reports its names.
	rc := &colRows{cols: []string{"id", "name"}}
	got, err := Columns(rc)
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(got) != 2 || got[0] != "id" || got[1] != "name" {
		t.Errorf("Columns = %v, want [id name]", got)
	}

	// A plain Rows reports ErrUnsupported — never a silent empty slice.
	if _, err := Columns(&fakeRows{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Columns on plain Rows: err = %v, want ErrUnsupported", err)
	}
}
