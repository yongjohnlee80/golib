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
// Every row is now the engine's real answer. Before the flags were removed,
// Copier, TwoPhaser and Upserter were true on EVERY row — GenericDialect
// implemented them and all four dialects embed it — so mysql claimed a COPY
// fast path it cannot perform. GenericDialect is included precisely because it
// must satisfy nothing: it provides the SQL shape, never a capability.
func TestCapabilityProbes(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    dao.Dialect
		want caps
	}{
		{"postgres", postgres.PostgresDialect{}, caps{returner: true, copier: true, twoPhase: true, upserter: true}},
		{"mysql", mysql.MysqlDialect{}, caps{upserter: true, lastID: true}},
		{"sqlite", sqlite.SqliteDialect{}, caps{returner: true, upserter: true}},
		{"generic", dao.GenericDialect{}, caps{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := probe(tc.d); got != tc.want {
				t.Errorf("capabilities = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestCapabilitiesAgreeWithFlags is GONE, and its absence is the result.
//
// While the boolean flags existed it compared each capability against the flag
// it replaced, and pinned the exact set that could not yet agree: Copier,
// TwoPhaser and Upserter were satisfied by EVERY dialect because
// GenericDialect implemented them and every dialect embeds it. mysql reported
// CopySupported() == false while satisfying Copier.
//
// Those flags are now removed and GenericDialect implements no capability, so
// there is nothing left to disagree. The matrix in TestCapabilityProbes above
// is the whole truth, and it changed in exactly the places that list predicted.

// The explicit declarations must render EXACTLY what the promoted generic
// implementation rendered, or step 3 changed behaviour while claiming not to.
func TestExplicitUpsertMatchesTheFormerGenericOutput(t *testing.T) {
	// The former generic output now lives in dao.StandardUpsertSuffix, which is
	// the single home the thin implementations delegate to.
	g := dao.GenericDialect{}
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
				want := dao.StandardUpsertSuffix(g, args.conflict, args.update)
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
