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
