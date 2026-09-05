package sqlite

import "testing"

// QuoteTable was previously unpinned for SQLite: dao/quote_table_test.go pins
// the opt-in against a fake dialect and dao/mysql pins MySQL's, but nothing
// exercised this one. A comment in dialect.go pointed here as its reference,
// which made the pointer half-true — the file covered the introspection
// capability and not this one.
func TestSqliteQuoteTable(t *testing.T) {
	var d SqliteDialect

	// Each dot-separated part is quoted SEPARATELY. Quoting the whole string
	// would name one table containing a dot, which is a different table.
	if got, want := d.QuoteTable("main.users"), `"main"."users"`; got != want {
		t.Errorf("QuoteTable(qualified) = %q, want %q", got, want)
	}
	// An unqualified name comes out exactly as QuoteIdent renders it.
	if got, want := d.QuoteTable("users"), d.QuoteIdent("users"); got != want {
		t.Errorf("QuoteTable(unqualified) = %q, want QuoteIdent's %q", got, want)
	}
	// Three parts, so the split is not special-cased to two.
	if got, want := d.QuoteTable("db.main.users"), `"db"."main"."users"`; got != want {
		t.Errorf("QuoteTable(three parts) = %q, want %q", got, want)
	}
}
