package sqlite

import (
	"context"
	"database/sql"
	"testing"
)

// A nil option is no option: Open skips it, as every constructor in golib does, and
// the options after it still apply.
func TestOpenSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Open with a nil option panicked: %v", r)
		}
	}()
	applied := false
	conn, err := Open(context.Background(), ":memory:", nil, func(*sql.DB) { applied = true })
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if !applied {
		t.Error("the option after the nil was not applied")
	}
}
