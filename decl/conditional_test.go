package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

func TestStringEqualityConditionalFollowsBothSourceValues(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, `Text { text: state === "raised" ? alert : normal }`,
		map[string]string{"state": "normal", "alert": "red", "normal": "plain"}, nil)
	if _, err := tr.SetSource("state", sv("raised")); err != nil {
		t.Fatal(err)
	}
	if got := applyLines(rec); len(got) != 1 || !strings.Contains(got[0], "red") {
		t.Fatalf("raised state did not take the alert branch: %v", got)
	}
	rec.trace = nil
	if _, err := tr.SetSource("alert", sv("scarlet")); err != nil {
		t.Fatal(err)
	}
	if got := applyLines(rec); len(got) != 1 || !strings.Contains(got[0], "scarlet") {
		t.Fatalf("chosen branch did not track its source: %v", got)
	}
	rec.trace = nil
	if _, err := tr.SetSource("state", sv("normal")); err != nil {
		t.Fatal(err)
	}
	if got := applyLines(rec); len(got) != 1 || !strings.Contains(got[0], "plain") {
		t.Fatalf("normal state did not take the other branch: %v", got)
	}
}

func TestStrictEqualityRefusesNonStringOperands(t *testing.T) {
	rec := newReactor()
	tr := decl.New(rec)
	spec, err := (qml.QML{}).Parse([]byte(`Text { text: 1 === "1" ? "bad" : "good" }`))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(spec); err == nil || !strings.Contains(err.Error(), "requires two strings") {
		t.Fatalf("numeric-string equality was coerced or misdiagnosed: %v", err)
	}
}

func TestConditionalRequiresBooleanAndChecksBothBranches(t *testing.T) {
	for _, tc := range []struct{ src, diagnostic string }{
		{`Text { text: "nonbool" ? "bad" : "good" }`, "requires a boolean condition"},
		{`Text { text: true ? "ok" : unknown }`, "unbound name"},
		{`Text { text: false ? unknown : "ok" }`, "unbound name"},
	} {
		tr := decl.New(newReactor())
		spec, err := (qml.QML{}).Parse([]byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Mount(spec); err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
			t.Errorf("%s: invalid condition or unreachable branch was not checked: %v", tc.src, err)
		}
	}
}

func TestConditionalEvaluatesOnlyTheChosenPureBranch(t *testing.T) {
	var chosen, skipped int
	values := map[string]decl.ValueFunc{
		"yes": func([]qml.SpecValue) (qml.SpecValue, error) { chosen++; return sv("yes"), nil },
		"no":  func([]qml.SpecValue) (qml.SpecValue, error) { skipped++; return sv("no"), nil },
	}
	rec := newReactor()
	tree(t, rec, `Text { text: true ? yes() : no() }`, nil, values)
	if chosen != 1 || skipped != 0 {
		t.Fatalf("conditional evaluated both branches: chosen %d, skipped %d", chosen, skipped)
	}
}

func TestTemplateEvaluationRefusesNonterminalLocalAndAValueCalledAsFunction(t *testing.T) {
	tr := decl.New(newReactor())
	for _, tc := range []struct {
		expr  string
		local qml.SpecValue
	}{
		{`Text { text: local }`, qml.SpecValue{Kind: qml.SpecValueRef, Raw: "somewhere"}},
		{`Text { text: local() }`, sv("ordinary value")},
	} {
		spec, err := (qml.QML{}).Parse([]byte(tc.expr))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tr.EvaluateWith(spec.Root.Props[0].Value, map[string]qml.SpecValue{"local": tc.local}); err == nil || !strings.Contains(err.Error(), "cannot be called") {
			t.Errorf("%s: invalid local was accepted: %v", tc.expr, err)
		}
	}
}
