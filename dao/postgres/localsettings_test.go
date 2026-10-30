package postgres

import (
	"errors"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// D2: the allowlist, each name's domain, and canonical values. The live cell 7
// checks the bounds against the server's own pg_settings.
func TestCheckLocalSetting(t *testing.T) {
	var d PostgresDialect
	for _, tc := range []struct {
		s  dao.LocalSetting
		ok bool
	}{
		{PlanCacheMode("auto"), true},
		{PlanCacheMode("force_custom_plan"), true},
		{PlanCacheMode("force_generic_plan"), true},
		{PlanCacheMode("custom"), false},
		{HNSWIterativeScan("off"), true},
		{HNSWIterativeScan("relaxed_order"), true},
		{HNSWIterativeScan("strict_order"), true},
		{HNSWIterativeScan("fast"), false},
		{HNSWEfSearch(1), true},
		{HNSWEfSearch(1000), true},
		{HNSWEfSearch(0), false},
		{HNSWEfSearch(1001), false},
		{dao.LocalSetting{Name: "hnsw.ef_search", Value: "05"}, false},
		{dao.LocalSetting{Name: "hnsw.ef_search", Value: " 5"}, false},
		{dao.LocalSetting{Name: "hnsw.ef_search", Value: "1e2"}, false},
		{dao.LocalSetting{Name: "hnsw.ef_search", Value: "5.0"}, false},
		{HNSWMaxScanTuples(1), true},
		{HNSWMaxScanTuples(2147483647), true},
		{HNSWMaxScanTuples(0), false},
		{HNSWMaxScanTuples(2147483648), false},
		{HNSWScanMemMultiplier(1), true},
		{HNSWScanMemMultiplier(1.5), true},
		{HNSWScanMemMultiplier(1000), true},
		{HNSWScanMemMultiplier(0.5), false},
		{HNSWScanMemMultiplier(1000.5), false},
		{dao.LocalSetting{Name: "hnsw.scan_mem_multiplier", Value: "1e1"}, false},
		{dao.LocalSetting{Name: "hnsw.scan_mem_multiplier", Value: "NaN"}, false},
		{dao.LocalSetting{Name: "hnsw.scan_mem_multiplier", Value: "2.50"}, false},
		{dao.LocalSetting{Name: "role", Value: "postgres"}, false},
		{dao.LocalSetting{Name: "search_path", Value: "public"}, false},
		{dao.LocalSetting{Name: "PLAN_CACHE_MODE", Value: "auto"}, false},
	} {
		err := d.CheckLocalSetting(tc.s)
		if tc.ok && err != nil {
			t.Errorf("%s = %q: %v; want accepted", tc.s.Name, tc.s.Value, err)
		}
		if !tc.ok && !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%s = %q: %v; want ErrInvalidArgument", tc.s.Name, tc.s.Value, err)
		}
	}
}

// D1: one statement each, names and values bound; the previous values read in
// a MATERIALIZED CTE before the outer select sets them, with is_local true.
func TestLocalSettingsSQL(t *testing.T) {
	var d PostgresDialect
	q, args := d.LocalSettingsSQL([]dao.LocalSetting{HNSWEfSearch(123), PlanCacheMode("auto")})
	want := "WITH prev AS MATERIALIZED (SELECT current_setting($1, true) AS p1, current_setting($3, true) AS p2) " +
		"SELECT p1, p2, set_config($1, $2, true), set_config($3, $4, true) FROM prev"
	if q != want {
		t.Errorf("settings SQL:\n got  %s\n want %s", q, want)
	}
	if want := []any{"hnsw.ef_search", "123", "plan_cache_mode", "auto"}; !slices.Equal(args, want) {
		t.Errorf("settings args = %v; want %v", args, want)
	}

	prev := "auto"
	q, args = d.LocalRestoreSQL([]dao.PriorSetting{{Name: "hnsw.ef_search"}, {Name: "plan_cache_mode", Value: &prev}})
	if want := "SELECT set_config($1, $2, true), set_config($3, $4, true)"; q != want {
		t.Errorf("restore SQL:\n got  %s\n want %s", q, want)
	}
	if want := []any{"hnsw.ef_search", nil, "plan_cache_mode", "auto"}; !slices.Equal(args, want) {
		t.Errorf("restore args = %v; want %v", args, want)
	}
}
