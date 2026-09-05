package dao_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/dao/mysql"
	"github.com/yongjohnlee80/golib/dao/postgres"
	"github.com/yongjohnlee80/golib/dao/sqlite"
)

// These tests exist for the WINDOW in which the capability interfaces and the
// boolean flags both exist. They are what makes the migration provable rather
// than plausible: while both are present, the two can be compared, and any
// disagreement is a behaviour change that would otherwise ship silently.
//
// The positive probes below must pass IDENTICALLY before and after the flags
// are removed. That is the point of them. If one starts failing after the
// removal, an engine lost a capability it used to have — which is exactly the
// regression that removing an inherited default causes, and it compiles green.

type caps struct {
	returner bool
	copier   bool
	twoPhase bool
	upserter bool
	lastID   bool
}

func probe(d dao.Dialect) caps {
	var c caps
	_, c.returner = d.(dao.Returner)
	_, c.copier = d.(dao.Copier)
	_, c.twoPhase = d.(dao.TwoPhaser)
	_, c.upserter = d.(dao.Upserter)
	_, c.lastID = d.(dao.LastInsertIDReader)
	return c
}

// TestCapabilityProbes pins what each dialect satisfies RIGHT NOW, during the
// window where the flags and the interfaces coexist.
//
// Three of the five are currently satisfied by EVERY dialect and say nothing
// about it: dao.GenericDialect declares Copy, the two-phase trio and
// BuildUpsertSuffix, and all four dialects embed it, so Copier, TwoPhaser and
// Upserter are granted by PROMOTION regardless of what the engine can actually
// do. mysql and sqlite report CopySupported() == false and
// TwoPhaseSupported() == false while satisfying both interfaces.
//
// That is the promotion hazard this ADR removes, measured rather than
// predicted, and it is wider than the RETURNING case that prompted the
// sequencing split. Only Returner and LastInsertIDReader are meaningful today,
// because their method names are new and GenericDialect does not have them.
func TestCapabilityProbes(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    dao.Dialect
		want caps
	}{
		// copier/twoPhase/upserter are true everywhere below by promotion.
		{"postgres", postgres.PostgresDialect{}, caps{returner: true, copier: true, twoPhase: true, upserter: true}},
		{"mysql", mysql.MysqlDialect{}, caps{copier: true, twoPhase: true, upserter: true, lastID: true}},
		{"sqlite", sqlite.SqliteDialect{}, caps{returner: true, copier: true, twoPhase: true, upserter: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := probe(tc.d); got != tc.want {
				t.Errorf("capabilities = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestCapabilitiesAgreeWithFlags is the migration's proof, and it can only be
// written while both the flags and the interfaces exist.
//
// For the two capabilities whose names are NEW, the interface and the flag must
// agree exactly — a disagreement there is a behaviour change.
//
// For the three that GenericDialect already implements, they cannot agree yet,
// and this test pins the EXACT set of disagreements rather than tolerating them
// loosely. That list is the step-5 checklist: when GenericDialect stops
// implementing capabilities, every one of these must flip to agreement, and any
// disagreement NOT listed here is a defect today.
func TestCapabilitiesAgreeWithFlags(t *testing.T) {
	// dialect -> capabilities currently granted by promotion despite a false flag.
	promoted := map[string][]string{
		"postgres": nil,
		"mysql":    {"Copier", "TwoPhaser"},
		"sqlite":   {"Copier", "TwoPhaser"},
		"generic":  {"Copier", "TwoPhaser"},
	}
	has := func(list []string, name string) bool {
		for _, s := range list {
			if s == name {
				return true
			}
		}
		return false
	}

	for _, tc := range []struct {
		name string
		d    dao.Dialect
	}{
		{"postgres", postgres.PostgresDialect{}},
		{"mysql", mysql.MysqlDialect{}},
		{"sqlite", sqlite.SqliteDialect{}},
		{"generic", dao.GenericDialect{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := probe(tc.d)
			exc := promoted[tc.name]

			// The clean two: exact agreement required.
			if c.lastID != tc.d.SupportsLastInsertID() {
				t.Errorf("LastInsertIDReader=%v but SupportsLastInsertID()=%v", c.lastID, tc.d.SupportsLastInsertID())
			}
			// Returner is clean in one direction only: GenericDialect reports
			// SupportsReturning() == true with nothing behind it, which is
			// exactly the inherited-capability shape that made sqlite's
			// RETURNING support removable by accident.
			if tc.name != "generic" && c.returner != tc.d.SupportsReturning() {
				t.Errorf("Returner=%v but SupportsReturning()=%v", c.returner, tc.d.SupportsReturning())
			}

			check := func(name string, got, flag bool) {
				switch {
				case got == flag:
					return
				case got && !flag && has(exc, name):
					return // known promotion grant, listed above
				default:
					t.Errorf("%s=%v but its flag=%v, and this disagreement is not in the "+
						"promotion list for %s", name, got, flag, tc.name)
				}
			}
			check("Copier", c.copier, tc.d.CopySupported())
			check("TwoPhaser", c.twoPhase, tc.d.TwoPhaseSupported())
			// Upserter is granted to everything, including where the flag is
			// false; asserted as always-true so a change is visible.
			if !c.upserter {
				t.Errorf("Upserter=false; GenericDialect declares BuildUpsertSuffix and every "+
					"dialect embeds it, so this cannot be false yet (flag says %v)", tc.d.SupportsUpsert())
			}
		})
	}
}

// The explicit declarations must render EXACTLY what the promoted generic
// implementation rendered, or step 3 changed behaviour while claiming not to.
func TestExplicitUpsertMatchesTheFormerGenericOutput(t *testing.T) {
	var g dao.GenericDialect
	for _, tc := range []struct {
		name string
		d    dao.Upserter
	}{
		{"postgres", postgres.PostgresDialect{}},
		{"sqlite", sqlite.SqliteDialect{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, args := range []struct{ conflict, update []string }{
				{nil, nil},
				{[]string{"id"}, nil},
				{[]string{"id"}, []string{"name", "email"}},
				{[]string{"a", "b"}, []string{"c"}},
			} {
				want := g.BuildUpsertSuffix(args.conflict, args.update)
				if got := tc.d.BuildUpsertSuffix(args.conflict, args.update); got != want {
					t.Errorf("BuildUpsertSuffix(%v, %v) = %q, want the former generic output %q",
						args.conflict, args.update, got, want)
				}
			}
		})
	}
}

// The RETURNING clause the capability renders must match what the builder
// produced from the flag.
func TestReturningClauseMatchesTheBuilderOutput(t *testing.T) {
	var d postgres.PostgresDialect
	quoted := d.QuoteIdent("id")
	if got, want := d.ReturningClause(quoted), " RETURNING "+quoted; got != want {
		t.Errorf("ReturningClause = %q, want %q", got, want)
	}
	// An engine with no id column renders nothing rather than a dangling clause.
	if got := d.ReturningClause(""); got != "" {
		t.Errorf("ReturningClause(\"\") = %q, want empty", got)
	}
}
