package sqlite

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// The capability must be discoverable, or dao.Str will never call it.
var _ dao.StringQuoter = SqliteDialect{}

func TestQuoteString(t *testing.T) {
	d := SqliteDialect{}

	t.Run("a doubled quote is the only escape", func(t *testing.T) {
		got, err := d.QuoteString("O'Brien")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := `'O''Brien'`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("a backslash is an ordinary character", func(t *testing.T) {
		// This is the half that differs from MySQL, and the reason MySQL does
		// NOT implement this capability: under standard_conforming_strings a
		// backslash means itself, so escaping it would change the value.
		got, err := d.QuoteString(`C:\Users`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := `'C:\Users'`; got != want {
			t.Errorf("got %s, want %s — a backslash must not be doubled", got, want)
		}
	})

	t.Run("a NUL byte is refused rather than truncated", func(t *testing.T) {
		_, err := d.QuoteString("a\x00b")
		if err == nil {
			t.Fatal("a NUL byte has no representation and must be refused")
		}
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("the refusal must carry ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("a literal cannot escape its own quotes", func(t *testing.T) {
		got, err := d.QuoteString(`'; DROP TABLE artist; --`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Count(got, "'")%2 != 0 {
			t.Errorf("unbalanced quotes let the literal end early: %s", got)
		}
		if inner := got[1 : len(got)-1]; strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
			t.Errorf("an unescaped quote survives inside the literal: %s", got)
		}
	})
}
