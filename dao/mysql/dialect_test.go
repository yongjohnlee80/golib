package mysql

import (
	"database/sql"
	"errors"
	"testing"

	drv "github.com/go-sql-driver/mysql"

	"github.com/yongjohnlee80/golib/dao"
)

// *sql.Rows must keep satisfying dao.RowsColumns (ADR-0012) — a future
// wrapper around the stdlib rows would silently drop the capability.
var _ dao.RowsColumns = (*sql.Rows)(nil)

func TestMysqlDialect_Basics(t *testing.T) {
	t.Parallel()
	d := MysqlDialect{}

	if d.Name() != "mysql" {
		t.Errorf("Name = %q", d.Name())
	}
	if d.Placeholder(3) != "?" {
		t.Errorf("Placeholder = %q, want ?", d.Placeholder(3))
	}
	if d.MaxBindParams() != 65535 {
		t.Errorf("MaxBindParams = %d, want 65535", d.MaxBindParams())
	}
	if got, want := d.QuoteIdent("na`me"), "`na``me`"; got != want {
		t.Errorf("QuoteIdent = %q, want %q", got, want)
	}
	if got, want := d.QuoteTable("app.users"), "`app`.`users`"; got != want {
		t.Errorf("QuoteTable = %q, want %q", got, want)
	}
	if got, want := d.QuoteTable("users"), "`users`"; got != want {
		t.Errorf("QuoteTable unqualified = %q, want %q", got, want)
	}
}

func TestMysqlDialect_CapabilityProfile(t *testing.T) {
	t.Parallel()
	var d dao.Dialect = MysqlDialect{}

	if d.SupportsReturning() {
		t.Error("SupportsReturning = true, want false")
	}
	if !d.SupportsLastInsertID() {
		t.Error("SupportsLastInsertID = false, want true (ADR-0008 §2.6)")
	}
	if !d.SupportsTransactions() || !d.SupportsUpsert() {
		t.Error("transactions/upsert must stay supported (GenericDialect defaults)")
	}
	if d.CopySupported() || d.TwoPhaseSupported() {
		t.Error("COPY/two-phase must stay unsupported")
	}
	if !d.SupportsIntrospection() {
		t.Error("SupportsIntrospection = false, want true (ADR-0013)")
	}
}

func TestMysqlDialect_UpsertSuffix(t *testing.T) {
	t.Parallel()
	d := MysqlDialect{}

	// Upsert: one VALUES() assignment per update column.
	got := d.BuildUpsertSuffix([]string{"uri"}, []string{"name", "public"})
	want := "ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `public` = VALUES(`public`)"
	if got != want {
		t.Errorf("upsert suffix = %q, want %q", got, want)
	}

	// Conflict cols only: do-nothing via first-conflict-column self-assignment.
	if got, want := d.BuildUpsertSuffix([]string{"uri"}, nil), "ON DUPLICATE KEY UPDATE `uri` = `uri`"; got != want {
		t.Errorf("conflict-only suffix = %q, want %q", got, want)
	}

	// Skip-conflicts shape: the engine's insert-column hint feeds the idiom.
	if got, want := d.BuildUpsertSuffix(nil, []string{"name", "uri"}), "ON DUPLICATE KEY UPDATE `name` = `name`"; got != want {
		t.Errorf("skip-conflicts suffix = %q, want %q", got, want)
	}

	// Both empty: unreachable via the engine; loud (invalid) on direct misuse.
	if got, want := d.BuildUpsertSuffix(nil, nil), "ON DUPLICATE KEY UPDATE"; got != want {
		t.Errorf("empty suffix = %q, want %q", got, want)
	}
}

func TestTranslateError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		errno    uint16
		sentinel error
		kind     dao.ConstraintKind
	}{
		{1062, dao.ErrDuplicate, dao.Unique},
		{1586, dao.ErrDuplicate, dao.Unique},
		{1048, dao.ErrNotNull, dao.NotNull},
		{1451, dao.ErrForeignKey, dao.ForeignKey},
		{1452, dao.ErrForeignKey, dao.ForeignKey},
	}
	for _, tc := range cases {
		in := &drv.MySQLError{Number: tc.errno, Message: "boom"}
		out := translateError(in)
		if !errors.Is(out, tc.sentinel) {
			t.Errorf("errno %d: not errors.Is(%v)", tc.errno, tc.sentinel)
		}
		var ce *dao.ConstraintError
		if !errors.As(out, &ce) || ce.Kind != tc.kind {
			t.Errorf("errno %d: kind = %v, want %v", tc.errno, ce.Kind, tc.kind)
		}
	}

	// Check violations carry Kind Check and unwrap to the driver error.
	chk := translateError(&drv.MySQLError{Number: 3819, Message: "chk"})
	var ce *dao.ConstraintError
	if !errors.As(chk, &ce) || ce.Kind != dao.Check {
		t.Errorf("errno 3819: kind = %v, want Check", ce.Kind)
	}

	// Unrecognized errors pass through unchanged.
	plain := errors.New("plain")
	if translateError(plain) != plain {
		t.Error("unrecognized error was not passed through")
	}
	if translateError(nil) != nil {
		t.Error("nil error was not passed through")
	}
}
