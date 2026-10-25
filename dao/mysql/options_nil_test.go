package mysql

import (
	"context"
	"database/sql"
	"testing"
)

// A nil option is no option: Open and OpenNamed skip it, as every constructor in golib does, and the
// options after it still apply.
func TestTheConstructorsSkipANilOption(t *testing.T) {
	// No server answers: each constructor applies its options, then fails to reach one.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, build := range map[string]func(applied *bool){
		"Open": func(applied *bool) {
			if conn, err := Open(ctx, "u:p@tcp(127.0.0.1:1)/d", nil, func(*sql.DB) { *applied = true }); err == nil {
				conn.Close()
			}
		},
		"OpenNamed": func(applied *bool) {
			if conn, err := OpenNamed(ctx, "n", "u:p@tcp(127.0.0.1:1)/d", nil, func(*sql.DB) { *applied = true }); err == nil {
				conn.Close()
			}
		},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s with a nil option panicked: %v", name, r)
				}
			}()
			applied := false
			build(&applied)
			if !applied {
				t.Errorf("%s: the option after the nil was not applied", name)
			}
		}()
	}
}
