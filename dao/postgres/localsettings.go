package postgres

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

var _ dao.LocalSetter = PostgresDialect{}

// The settings dao.WithLocalSettings may make on PostgreSQL. set_config sets
// anything the role may set, role and search_path included, so a name off this
// list is refused rather than sent. The bounds are pgvector 0.8's and
// PostgreSQL 16+'s; an integration cell checks them against the server's
// pg_settings. Adding a name needs its domain and a cell.
const (
	settingPlanCacheMode         = "plan_cache_mode"
	settingHNSWIterativeScan     = "hnsw.iterative_scan"
	settingHNSWEfSearch          = "hnsw.ef_search"
	settingHNSWMaxScanTuples     = "hnsw.max_scan_tuples"
	settingHNSWScanMemMultiplier = "hnsw.scan_mem_multiplier"
)

// localDomain is one allowlisted name's domain: its members for an enum, else
// its bounds, as an integer or a number.
type localDomain struct {
	enum     []string
	integer  bool
	min, max float64
}

var localSettings = map[string]localDomain{
	settingPlanCacheMode:         {enum: []string{"auto", "force_custom_plan", "force_generic_plan"}},
	settingHNSWIterativeScan:     {enum: []string{"off", "relaxed_order", "strict_order"}},
	settingHNSWEfSearch:          {integer: true, min: 1, max: 1000},
	settingHNSWMaxScanTuples:     {integer: true, min: 1, max: math.MaxInt32},
	settingHNSWScanMemMultiplier: {min: 1, max: 1000},
}

// PlanCacheMode is plan_cache_mode: auto, force_custom_plan or
// force_generic_plan.
func PlanCacheMode(mode string) dao.LocalSetting {
	return dao.LocalSetting{Name: settingPlanCacheMode, Value: mode}
}

// HNSWIterativeScan is pgvector's hnsw.iterative_scan: off, relaxed_order or
// strict_order.
func HNSWIterativeScan(mode string) dao.LocalSetting {
	return dao.LocalSetting{Name: settingHNSWIterativeScan, Value: mode}
}

// HNSWEfSearch is pgvector's hnsw.ef_search, 1 to 1000.
func HNSWEfSearch(n int) dao.LocalSetting {
	return dao.LocalSetting{Name: settingHNSWEfSearch, Value: strconv.Itoa(n)}
}

// HNSWMaxScanTuples is pgvector's hnsw.max_scan_tuples, 1 to 2147483647.
func HNSWMaxScanTuples(n int) dao.LocalSetting {
	return dao.LocalSetting{Name: settingHNSWMaxScanTuples, Value: strconv.Itoa(n)}
}

// HNSWScanMemMultiplier is pgvector's hnsw.scan_mem_multiplier, 1 to 1000.
func HNSWScanMemMultiplier(x float64) dao.LocalSetting {
	return dao.LocalSetting{Name: settingHNSWScanMemMultiplier, Value: strconv.FormatFloat(x, 'f', -1, 64)}
}

// CheckLocalSetting implements dao.LocalSetter: the name must be on the
// allowlist, and the value in its domain, written canonically (an integer in
// decimal, a number with no exponent, an enum as one of its members).
func (PostgresDialect) CheckLocalSetting(s dao.LocalSetting) error {
	dom, ok := localSettings[s.Name]
	if !ok {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: %q is not a setting dao may make (the allowlist: %s)",
			s.Name, strings.Join(slices.Sorted(maps.Keys(localSettings)), ", "))
	}
	if dom.enum != nil {
		if !slices.Contains(dom.enum, s.Value) {
			return errs.Wrap(errs.ErrInvalidArgument, "postgres: %s = %q: want one of %s",
				s.Name, s.Value, strings.Join(dom.enum, ", "))
		}
		return nil
	}
	var x float64
	if dom.integer {
		n, err := strconv.ParseInt(s.Value, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != s.Value {
			return errs.Wrap(errs.ErrInvalidArgument, "postgres: %s = %q: want an integer", s.Name, s.Value)
		}
		x = float64(n)
	} else {
		f, err := strconv.ParseFloat(s.Value, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || strconv.FormatFloat(f, 'f', -1, 64) != s.Value {
			return errs.Wrap(errs.ErrInvalidArgument, "postgres: %s = %q: want a number with no exponent", s.Name, s.Value)
		}
		x = f
	}
	if x < dom.min || x > dom.max {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: %s = %s: want %s to %s", s.Name, s.Value,
			strconv.FormatFloat(dom.min, 'f', -1, 64), strconv.FormatFloat(dom.max, 'f', -1, 64))
	}
	return nil
}

// LocalSettingsSQL implements dao.LocalSetter. The MATERIALIZED CTE is
// evaluated before the outer select's set_config calls, so each
// current_setting reads the value in force before the statement, whatever an
// earlier statement of the transaction set; current_setting(…, true) answers
// NULL, never an error, for a name the server has not defined (a pgvector name
// before the library loads). set_config(…, true) is SET LOCAL with bound
// parameters.
func (PostgresDialect) LocalSettingsSQL(settings []dao.LocalSetting) (string, []any) {
	args := make([]any, 0, 2*len(settings))
	prev := make([]string, len(settings))
	cols := make([]string, len(settings))
	set := make([]string, len(settings))
	for i, s := range settings {
		n, v := len(args)+1, len(args)+2
		args = append(args, s.Name, s.Value)
		prev[i] = fmt.Sprintf("current_setting($%d, true) AS p%d", n, i+1)
		cols[i] = fmt.Sprintf("p%d", i+1)
		set[i] = fmt.Sprintf("set_config($%d, $%d, true)", n, v)
	}
	return "WITH prev AS MATERIALIZED (SELECT " + strings.Join(prev, ", ") + ") SELECT " +
		strings.Join(cols, ", ") + ", " + strings.Join(set, ", ") + " FROM prev", args
}

// LocalRestoreSQL implements dao.LocalSetter: set_config(…, true) for each
// name, a NULL value resetting it to the value it would have with no SET in
// the transaction.
func (PostgresDialect) LocalRestoreSQL(previous []dao.PriorSetting) (string, []any) {
	args := make([]any, 0, 2*len(previous))
	set := make([]string, len(previous))
	for i, p := range previous {
		var v any
		if p.Value != nil {
			v = *p.Value
		}
		set[i] = fmt.Sprintf("set_config($%d, $%d, true)", len(args)+1, len(args)+2)
		args = append(args, p.Name, v)
	}
	return "SELECT " + strings.Join(set, ", "), args
}

// AbortsTransaction implements dao.LocalSetter: an error the server reported
// (an ErrorResponse, *pgconn.PgError) aborts PostgreSQL's transaction, which
// ends the settings; any other (a scan or decode error, an argument pgx could
// not encode) left the transaction alive, so the settings are restored.
func (PostgresDialect) AbortsTransaction(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg)
}
