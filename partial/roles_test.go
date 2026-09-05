package partial

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// setterOnly implements dao.Setter and NOTHING else — no Get, no Insert, no
// Name, no connection, no schema. It exists to prove that ApplyRules really
// did narrow: a value that cannot possibly satisfy the 24-method dao.DAO is
// an accepted argument.
type setterOnly struct {
	got   map[relField]dao.Rule
	calls int
}

func (s *setterOnly) Set(field relField, value any) dao.DAO[*release, relField, string] {
	return nil
}

func (s *setterOnly) SetMap(values map[relField]any) dao.DAO[*release, relField, string] {
	return nil
}

func (s *setterOnly) Clear(field relField) dao.DAO[*release, relField, string] {
	return nil
}

func (s *setterOnly) SetRules(rules map[relField]dao.Rule) dao.DAO[*release, relField, string] {
	s.calls++
	s.got = rules
	return nil
}

func TestApplyRules_AcceptsTheNarrowRole(t *testing.T) {
	t.Parallel()

	f := &setterOnly{}

	// Specificity check. If a later edit fattened setterOnly into something
	// that satisfies the whole DAO, every assertion below would still pass
	// while proving nothing about narrowing. Fail loudly instead.
	if _, isDAO := any(f).(dao.DAO[*release, relField, string]); isDAO {
		t.Fatal("setterOnly satisfies the full DAO — this test no longer demonstrates narrowing")
	}

	p, err := Bind[release]([]byte(`{"title":"x","upc":null}`))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// No explicit type arguments. Go must infer R, C, ID and T through
	// Setter's own method signatures; a role whose methods did not mention
	// them would not compile here.
	if _, err := ApplyRules(f, p); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}

	if f.calls != 1 {
		t.Fatalf("SetRules called %d times, want 1", f.calls)
	}
	want := map[relField]dao.Rule{
		"title": dao.Write("x"),
		"upc":   dao.Clear(),
	}
	if !reflect.DeepEqual(f.got, want) {
		t.Errorf("staged rules = %#v, want %#v", f.got, want)
	}
}

// An unusable patch must stage nothing. The error path re-widens the narrow
// argument by calling SetRules with an empty map, so it is observable here as
// one call carrying no rules — on a real DAO that loop body never runs.
func TestApplyRules_ErrorPathStagesNothing(t *testing.T) {
	t.Parallel()

	f := &setterOnly{}

	p, err := Bind[release]([]byte(`{"title":"x"}`))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	p.Set("no-such-field", "boom") // sticky ErrUnknownField

	// Positive control for the arrangement: without a failing patch the
	// assertions below would be measuring the happy path.
	if _, err := p.Rules(); err == nil {
		t.Fatal("patch is not in an error state; the test would prove nothing")
	}

	if _, err := ApplyRules(f, p); err == nil {
		t.Fatal("ApplyRules returned nil error for an unusable patch")
	}
	if len(f.got) != 0 {
		t.Errorf("error path staged %d rules, want 0: %#v", len(f.got), f.got)
	}
}
