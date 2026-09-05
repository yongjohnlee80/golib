package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// A1b. autodb converts plain functions to this Option type
// (core/exec/conns.go). That conversion is what makes changing the Option type
// breaking rather than additive, so the connect-hook work must leave it
// compiling. This witness is the conversion form itself.
var _ = Option(func(db *sql.DB) { db.SetMaxOpenConns(1) })

// OpenHooked must accept a dao.ConnectHook and keep Option variadic after it.
// The behaviour needs a MySQL server, so what is asserted here is the shape —
// dao/internal/stdsql carries the behavioural tests against a real engine, and
// both engines run the same hook-invocation code.
var _ = func(hook dao.ConnectHook) (dao.DataConn, error) {
	return OpenHooked(context.Background(), "mysql", "u:p@tcp(127.0.0.1:3306)/d", hook,
		MaxOpenConns(1))
}

// A connect to a server that is not there must fail rather than hang or
// succeed, and it must not need the hook to have run.
func TestOpenHookedFailsWithoutAServer(t *testing.T) {
	ctx := context.Background()
	called := false

	_, err := OpenHooked(ctx, "mysql", "u:p@tcp(127.0.0.1:1)/d",
		func(context.Context, dao.ConnectedConn) error { called = true; return nil })
	if err == nil {
		t.Fatal("OpenHooked succeeded against a closed port")
	}
	if called {
		t.Error("the hook ran although no connection was established")
	}
}
