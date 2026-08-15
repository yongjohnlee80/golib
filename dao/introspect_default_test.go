package dao

import (
	"context"
	"errors"
	"testing"
)

func TestIntrospection_GenericDefaultsUnsupported(t *testing.T) {
	t.Parallel()
	conn := newConn()
	ctx := context.Background()

	if (GenericDialect{}).SupportsIntrospection() {
		t.Error("GenericDialect.SupportsIntrospection() = true, want false")
	}
	if _, err := ListSchemas(ctx, conn); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ListSchemas err = %v, want ErrUnsupported", err)
	}
	if _, err := ListTables(ctx, conn, "public"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ListTables err = %v, want ErrUnsupported", err)
	}
	if _, err := ListColumns(ctx, conn, "public", "artist"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ListColumns err = %v, want ErrUnsupported", err)
	}
}
