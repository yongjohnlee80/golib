package dao

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// quotingDialect states a rule; refusingDialect states none.
type quotingDialect struct{ GenericDialect }

func (quotingDialect) QuoteString(s string) (string, error) {
	if strings.IndexByte(s, 0) >= 0 {
		return "", errors.New("no representation for a NUL byte")
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

// Str asks the dialect, because only the dialect knows the rule.
func TestStr_QuotesThroughTheDialectWhenItCan(t *testing.T) {
	e := Str("O'Brien")
	got := e.render(quotingDialect{})
	if want := `'O''Brien'`; got != want {
		t.Errorf("render = %s, want %s", got, want)
	}

	// The property that matters: a quoted literal cannot end early. If it
	// could, everything after it would be SQL the caller did not write.
	hostile := Str(`'; DROP TABLE artist; --`)
	out := hostile.render(quotingDialect{})
	if strings.Count(out, "'")%2 != 0 {
		t.Errorf("the literal has unbalanced quotes and can escape its own string: %s", out)
	}
	if inner := out[1 : len(out)-1]; strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
		t.Errorf("an unescaped quote survives inside the literal: %s", out)
	}
}

// A dialect that CANNOT state its rule must refuse, not inherit a guess. That
// is the whole reason this is a capability rather than a Dialect method with a
// GenericDialect default.
func TestStr_RefusesWhenTheDialectStatesNoRule(t *testing.T) {
	for name, in := range map[string]string{
		"single quote":      "O'Brien",
		"backslash":         `C:\Users`,
		"control character": "a\x00b",
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				rec := recover()
				if rec == nil {
					t.Fatalf("Str(%q) must refuse on a dialect with no quoting rule", in)
				}
				var f errs.Fatal
				err, ok := rec.(error)
				if !ok || !errors.As(err, &f) {
					t.Fatalf("the refusal must carry an errs.Fatal, got %T", rec)
				}
				if f.Op != "dao.Str" {
					t.Errorf("Op = %q, want dao.Str", f.Op)
				}
				if !strings.Contains(f.Rule, "generic") {
					t.Errorf("the refusal must name the dialect that has no rule: %q", f.Rule)
				}
			}()
			_ = Str(in).render(GenericDialect{})
		})
	}
}

// A quoter is allowed to refuse, and the refusal must reach the caller as a
// contract failure rather than as SQL that means something shorter.
func TestStr_ADialectMayRefuseAnUnrepresentableString(t *testing.T) {
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("a quoter that cannot represent its input must not silently succeed")
		}
		var f errs.Fatal
		if err, ok := rec.(error); !ok || !errors.As(err, &f) {
			t.Fatalf("want an errs.Fatal, got %T", rec)
		}
		if !strings.Contains(f.Detail, "NUL") {
			t.Errorf("the dialect's own reason must survive: %q", f.Detail)
		}
	}()
	_ = Str("a\x00b").render(quotingDialect{})
}

// Coalesce routes a caller-supplied string through Str, which is the path the
// panic budget records as reachable.
func TestCoalesce_RoutesStringsThroughStr(t *testing.T) {
	got := Coalesce(T("artist", "name"), "O'Brien").render(quotingDialect{})
	if !strings.Contains(got, `'O''Brien'`) {
		t.Errorf("Coalesce must quote its string alternative through the dialect: %s", got)
	}
}
