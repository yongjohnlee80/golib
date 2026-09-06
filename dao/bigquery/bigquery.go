// Package bigquery is the golib/dao driver for Google BigQuery — a read-mostly,
// no-transaction OLAP store. It implements dao.DataConn over
// cloud.google.com/go/bigquery against the read-mostly / no-transaction driver
// contract (golib-dao ADR-0008).
//
// Reads (Select/Get/Count/Exists/Iterate) and DML writes (Insert/Update/Delete)
// work; transactions, Upsert, and the COPY fast-path return dao.ErrUnsupported.
// Insert returns the zero ID with a nil error (BigQuery has no server-generated
// id) — supply ids client-side (e.g. a UUID) for append-only tables.
//
// It is a separate Go module so the heavy GCP SDK is pulled only by callers that
// actually use BigQuery; the golib/dao core stays dependency-light.
package bigquery

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// Open opens a BigQuery dao.DataConn named "bigquery" for projectID, scoping
// unqualified table names to dataset. Pass option.ClientOption values (e.g.
// option.WithCredentialsFile) for auth.
func Open(ctx context.Context, projectID, dataset string, opts ...option.ClientOption) (dao.DataConn, error) {
	return OpenNamed(ctx, dao.DialectBigQuery, projectID, dataset, opts...)
}

// OpenNamed opens a BigQuery dao.DataConn with an explicit name (the key dao uses
// for transaction contexts and logs). BigQuery is no-transaction, so a tx-bound
// DAO on this connection fails with dao.ErrUnsupported on first touch.
func OpenNamed(ctx context.Context, name, projectID, dataset string, opts ...option.ClientOption) (dao.DataConn, error) {
	client, err := bigquery.NewClient(ctx, projectID, opts...)
	if err != nil {
		return nil, err
	}
	return &bqConn{client: client, name: name, projectID: projectID, dataset: dataset}, nil
}

// bqConn is a dao.DataConn backed by a bigquery.Client.
type bqConn struct {
	client    *bigquery.Client
	name      string
	projectID string
	dataset   string
}

// compile-time: *bqConn satisfies dao.DataConn.
var _ dao.DataConn = (*bqConn)(nil)

// query builds a *bigquery.Query for sql, scoped to the default dataset, with the
// positional args bound as unnamed (positional) query parameters.
func (c *bqConn) query(sql string, args []any) *bigquery.Query {
	q := c.client.Query(sql)
	if c.dataset != "" {
		q.DefaultProjectID = c.projectID
		q.DefaultDatasetID = c.dataset
	}
	if len(args) > 0 {
		params := make([]bigquery.QueryParameter, len(args))
		for i, a := range args {
			params[i] = bigquery.QueryParameter{Value: a} // empty Name => positional
		}
		q.Parameters = params
	}
	return q
}

// run executes sql as a job and waits for completion, returning a clean error on
// failure. read controls whether a row iterator is returned (reads) or the job
// status (DML).
func (c *bqConn) run(ctx context.Context, sql string, args []any) (*bigquery.Job, *bigquery.JobStatus, error) {
	job, err := c.query(sql, args).Run(ctx)
	if err != nil {
		return nil, nil, err
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := status.Err(); err != nil {
		return nil, nil, err
	}
	return job, status, nil
}

func (c *bqConn) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	job, _, err := c.run(ctx, q, args)
	if err != nil {
		return nil, err
	}
	it, err := job.Read(ctx)
	if err != nil {
		return nil, err
	}
	return &bqRows{it: it}, nil
}

func (c *bqConn) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	_, status, err := c.run(ctx, q, args)
	if err != nil {
		return nil, err
	}
	var affected int64
	if qs, ok := status.Statistics.Details.(*bigquery.QueryStatistics); ok {
		affected = qs.NumDMLAffectedRows
	}
	return bqResult{affected: affected}, nil
}

func (c *bqConn) Dialect() dao.Dialect { return BigQueryDialect{} }

// Begin returns dao.ErrUnsupported: BigQuery has no interactive transactions.
func (c *bqConn) Begin(context.Context) (dao.TxConn, error) {
	return nil, fmt.Errorf("bigquery: %w: interactive transactions", dao.ErrUnsupported)
}

func (c *bqConn) Name() string { return c.name }
func (c *bqConn) Close() error { return c.client.Close() }

// bqRows adapts a *bigquery.RowIterator to dao.Rows. Each row is read into a
// []bigquery.Value and assigned positionally into the Scan destinations — the
// reflection adapter that bridges BigQuery's single-dest Next(dest) to dao's
// variadic Scan(dest...any).
type bqRows struct {
	it      *bigquery.RowIterator
	current []bigquery.Value
	err     error
	done    bool
}

func (r *bqRows) Next() bool {
	if r.done || r.err != nil {
		return false
	}
	var vals []bigquery.Value
	if err := r.it.Next(&vals); err != nil {
		if errors.Is(err, iterator.Done) {
			r.done = true
		} else {
			r.err = err
		}
		return false
	}
	r.current = vals
	return true
}

func (r *bqRows) Scan(dest ...any) error {
	// A count mismatch is a schema/projection bug: silently zeroing missing
	// targets or dropping extra columns would corrupt reads (must-fix from the
	// 2026-06-23 review).
	if len(dest) != len(r.current) {
		return errs.Wrap(errs.ErrInvalidArgument, "bigquery: scan: %d destinations for %d columns", len(dest), len(r.current))
	}
	for i, d := range dest {
		if err := assign(d, r.current[i]); err != nil {
			return fmt.Errorf("bigquery: scan column %d: %w", i, err)
		}
	}
	return nil
}

func (r *bqRows) Close() error { return nil } // BigQuery iterators need no close
func (r *bqRows) Err() error   { return r.err }

// bqResult adapts BigQuery DML job stats to dao.Result.
type bqResult struct{ affected int64 }

func (r bqResult) RowsAffected() (int64, error) { return r.affected, nil }

// LastInsertId is unsupported (no server-generated ids). The dao engine never
// calls it for this dialect (SupportsLastInsertID is false); the error is
// defensive.
func (bqResult) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("bigquery: %w: LastInsertId (no server-generated ids)", dao.ErrUnsupported)
}

var timeType = reflect.TypeOf(time.Time{})

// assign sets *dest from a bigquery.Value, handling the common BigQuery→Go type
// mappings: direct assignment, numeric conversion (INT64/FLOAT64 → Go numeric
// kinds), DATE/DATETIME → time.Time, and NUMERIC/BIGNUMERIC (*big.Rat) → string.
// A nil value leaves the destination at its zero value.
func assign(dest any, val bigquery.Value) error {
	if val == nil {
		return nil
	}
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return errs.Wrap(errs.ErrInvalidArgument, "bigquery: scan destination must be a non-nil pointer, got %T", dest)
	}
	target := dv.Elem()
	if !target.CanSet() {
		return errs.Wrap(errs.ErrInvalidArgument, "bigquery: scan destination %T is not settable", dest)
	}
	tt := target.Type()
	sv := reflect.ValueOf(val)

	// Direct assignment (string, bool, float64, int64, []byte, time.Time, ...).
	if sv.Type().AssignableTo(tt) {
		target.Set(sv)
		return nil
	}

	// BigQuery temporal types → time.Time.
	if tt == timeType {
		switch v := val.(type) {
		case civil.Date:
			target.Set(reflect.ValueOf(time.Date(v.Year, v.Month, v.Day, 0, 0, 0, 0, time.UTC)))
			return nil
		case civil.DateTime:
			target.Set(reflect.ValueOf(time.Date(v.Date.Year, v.Date.Month, v.Date.Day,
				v.Time.Hour, v.Time.Minute, v.Time.Second, v.Time.Nanosecond, time.UTC)))
			return nil
		}
	}

	// NUMERIC / BIGNUMERIC arrive as *big.Rat; render to a string target.
	if rat, ok := val.(*big.Rat); ok && tt.Kind() == reflect.String {
		target.SetString(rat.FloatString(38))
		return nil
	}

	// Numeric conversion (e.g. INT64 → int/uint, FLOAT64 → float32). Checked:
	// a value the target cannot represent exactly is an error, never a silent
	// wraparound or truncation (must-fix from the 2026-06-23 review).
	if isNumeric(sv.Kind()) && isNumeric(tt.Kind()) {
		return assignNumeric(target, sv, val)
	}

	return errs.Wrap(errs.ErrInvalidArgument, "bigquery: cannot assign a value of type %T to %s", val, tt)
}

// assignNumeric converts a numeric source value into a numeric target with
// range and exactness checks. BigQuery yields int64 (INT64) and float64
// (FLOAT64); the checks are written over the kind classes so any numeric pair
// is safe:
//
//   - integer → integer: range-checked (negative → unsigned and overflow fail)
//   - float → float: overflow-checked (e.g. 1e300 → float32 fails)
//   - float → integer: must be an exact integral value in range (7.0 ok, 7.5 not)
//   - integer → float: allowed (IEEE-754 may round above 2^53, as everywhere in Go)
func assignNumeric(target reflect.Value, sv reflect.Value, val bigquery.Value) error {
	tt := target.Type()
	fail := func() error {
		return errs.Wrap(errs.ErrInvalidArgument, "bigquery: value %v (%T) does not fit destination %s", val, val, tt)
	}
	switch {
	case isInt(sv.Kind()):
		v := sv.Int()
		switch {
		case isInt(tt.Kind()):
			if target.OverflowInt(v) {
				return fail()
			}
			target.SetInt(v)
		case isUint(tt.Kind()):
			if v < 0 || target.OverflowUint(uint64(v)) {
				return fail()
			}
			target.SetUint(uint64(v))
		default: // float target
			target.SetFloat(float64(v))
		}
	case isFloat(sv.Kind()):
		v := sv.Float()
		switch {
		case isFloat(tt.Kind()):
			if target.OverflowFloat(v) {
				return fail()
			}
			target.SetFloat(v)
		case isInt(tt.Kind()):
			if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
				return fail()
			}
			i := int64(v)
			if float64(i) != v || target.OverflowInt(i) {
				return fail()
			}
			target.SetInt(i)
		default: // uint target
			if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v < 0 {
				return fail()
			}
			u := uint64(v)
			if float64(u) != v || target.OverflowUint(u) {
				return fail()
			}
			target.SetUint(u)
		}
	case isUint(sv.Kind()):
		// Defensive: BigQuery never yields unsigned values, but stay total.
		v := sv.Uint()
		switch {
		case isUint(tt.Kind()):
			if target.OverflowUint(v) {
				return fail()
			}
			target.SetUint(v)
		case isInt(tt.Kind()):
			if v > math.MaxInt64 || target.OverflowInt(int64(v)) {
				return fail()
			}
			target.SetInt(int64(v))
		default:
			target.SetFloat(float64(v))
		}
	}
	return nil
}

func isInt(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

func isUint(k reflect.Kind) bool {
	switch k {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func isFloat(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

func isNumeric(k reflect.Kind) bool {
	return isInt(k) || isUint(k) || isFloat(k)
}
