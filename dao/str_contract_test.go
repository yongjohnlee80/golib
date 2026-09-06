package dao

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// Str inlines its argument as a SQL literal, so it refuses the three characters
// that have no portable inline escaping. The panic budget classifies these as a
// LATENT violation — reachable at runtime with a valid type, because Coalesce's
// string arm routes through Str — and the remedy is a design decision, not a
// sweep: there is no bind channel at schema-declaration time, and adding
// escaping to the library is a security decision of its own.
//
// What this pins is the contract as it stands, so the refusal is recognisable
// and the reason is readable rather than a sentence to match.
func TestStr_RefusesWhatItCannotEscape(t *testing.T) {
	cases := map[string]string{
		"single quote":      "O'Brien",
		"backslash":         `C:\Users`,
		"control character": "a\x00b",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				rec := recover()
				if rec == nil {
					t.Fatalf("Str(%q) must refuse: inlining it would produce SQL "+
						"whose meaning depends on the engine's escaping mode", in)
				}
				err, ok := rec.(error)
				if !ok {
					t.Fatalf("the refusal must carry a value, got %T", rec)
				}
				var f errs.Fatal
				if !errors.As(err, &f) {
					t.Fatalf("errors.As must recover the Fatal, got %v", err)
				}
				if f.Op != "dao.Str" {
					t.Errorf("Op = %q, want %q", f.Op, "dao.Str")
				}
				// The remedy belongs in the value, where a caller can read it,
				// not only in prose a human might see in a stack trace.
				if !strings.Contains(f.Detail, "dao.SQL") {
					t.Errorf("Detail must name the way out, got %q", f.Detail)
				}
			}()
			_ = Str(in)
		})
	}
}

// The reachable path the budget records, pinned so the LATENT classification
// stays honest: if Coalesce ever stops routing strings through Str, the
// violation row's reason is stale and should be re-read.
func TestCoalesce_RoutesStringsThroughStr(t *testing.T) {
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("Coalesce with a caller-supplied string containing a quote " +
				"must reach Str's refusal; if it no longer does, the panic budget's " +
				"LATENT reason for dao.Str is out of date")
		}
	}()
	_ = Coalesce(T("artist", "name"), "O'Brien")
}
