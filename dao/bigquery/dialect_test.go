package bigquery

import (
	"context"
	"errors"
	"math"
	"math/big"

	"testing"
	"time"

	gcpbq "cloud.google.com/go/bigquery"

	"cloud.google.com/go/civil"
	"github.com/yongjohnlee80/golib/dao"
)

// These tests need no BigQuery credentials: they exercise the dialect contract,
// the no-transaction gate, and the scan-adapter type conversions in isolation.

func TestDialect_CapabilityProfile(t *testing.T) {
	t.Parallel()

	var d dao.Dialect = BigQueryDialect{}
	if d.Name() != "bigquery" {
		t.Errorf("Name() = %q", d.Name())
	}
	if got := d.Placeholder(3); got != "?" {
		t.Errorf("Placeholder = %q, want ?", got)
	}
	if got := d.QuoteIdent("dataset.table"); got != "`dataset.table`" {
		t.Errorf("QuoteIdent = %q", got)
	}
	if got := d.QuoteIdent("we`ird"); got != "`weird`" {
		t.Errorf("QuoteIdent backtick strip = %q", got)
	}
	// BigQuery is an append-mostly OLAP store: it implements NO optional
	// capability, and the ABSENCE of each interface is how that is stated.
	// There is no flag to disagree with the implementation, and no inherited
	// default to grant something the engine cannot do — notably upsert, which
	// this dialect used to "support" by rendering an empty conflict clause,
	// turning an upsert into a silent plain INSERT.
	for name, satisfied := range map[string]bool{
		"Returner":           func() bool { _, ok := any(d).(dao.Returner); return ok }(),
		"Copier":             func() bool { _, ok := any(d).(dao.Copier); return ok }(),
		"TwoPhaser":          func() bool { _, ok := any(d).(dao.TwoPhaser); return ok }(),
		"Upserter":           func() bool { _, ok := any(d).(dao.Upserter); return ok }(),
		"LastInsertIDReader": func() bool { _, ok := any(d).(dao.LastInsertIDReader); return ok }(),
	} {
		if satisfied {
			t.Errorf("BigQueryDialect satisfies dao.%s; the no-transaction OLAP profile "+
				"implements no capability", name)
		}
	}
}

func TestBegin_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := (&bqConn{name: "bigquery"}).Begin(context.Background())
	if !errors.Is(err, dao.ErrUnsupported) {
		t.Fatalf("Begin err = %v, want dao.ErrUnsupported", err)
	}
}

func TestResult_LastInsertIDUnsupported(t *testing.T) {
	t.Parallel()

	if _, err := (bqResult{}).LastInsertId(); !errors.Is(err, dao.ErrUnsupported) {
		t.Errorf("LastInsertId err = %v, want dao.ErrUnsupported", err)
	}
	if n, err := (bqResult{affected: 5}).RowsAffected(); err != nil || n != 5 {
		t.Errorf("RowsAffected = %d, %v", n, err)
	}
}

func TestAssign_TypeConversions(t *testing.T) {
	t.Parallel()

	t.Run("string", func(t *testing.T) {
		var s string
		if err := assign(&s, "hello"); err != nil || s != "hello" {
			t.Fatalf("s=%q err=%v", s, err)
		}
	})
	t.Run("int64->int", func(t *testing.T) {
		var n int
		if err := assign(&n, int64(42)); err != nil || n != 42 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
	t.Run("int64->uint64", func(t *testing.T) {
		var n uint64
		if err := assign(&n, int64(7)); err != nil || n != 7 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
	t.Run("float64", func(t *testing.T) {
		var f float64
		if err := assign(&f, 3.5); err != nil || f != 3.5 {
			t.Fatalf("f=%v err=%v", f, err)
		}
	})
	t.Run("bool", func(t *testing.T) {
		var b bool
		if err := assign(&b, true); err != nil || !b {
			t.Fatalf("b=%v err=%v", b, err)
		}
	})
	t.Run("civil.Date->time.Time", func(t *testing.T) {
		var ts time.Time
		if err := assign(&ts, civil.Date{Year: 2026, Month: time.June, Day: 20}); err != nil {
			t.Fatalf("err=%v", err)
		}
		if ts.Year() != 2026 || ts.Month() != time.June || ts.Day() != 20 {
			t.Fatalf("ts=%v", ts)
		}
	})
	t.Run("time.Time direct", func(t *testing.T) {
		var ts time.Time
		want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := assign(&ts, want); err != nil || !ts.Equal(want) {
			t.Fatalf("ts=%v err=%v", ts, err)
		}
	})
	t.Run("bigRat->string", func(t *testing.T) {
		var s string
		if err := assign(&s, big.NewRat(1, 4)); err != nil {
			t.Fatalf("err=%v", err)
		}
		if s == "" || s[:3] != "0.2" {
			t.Fatalf("numeric string = %q", s)
		}
	})
	t.Run("nil leaves zero", func(t *testing.T) {
		s := "untouched"
		if err := assign(&s, nil); err != nil || s != "untouched" {
			t.Fatalf("s=%q err=%v", s, err)
		}
	})
	t.Run("mismatch errors", func(t *testing.T) {
		var n int
		if err := assign(&n, "not-a-number"); err == nil {
			t.Fatal("want error assigning string to int")
		}
	})
	t.Run("non-pointer errors", func(t *testing.T) {
		var n int
		if err := assign(n, int64(1)); err == nil {
			t.Fatal("want error for non-pointer destination")
		}
	})
}

// TestAssign_CheckedNumericConversions pins the checked-conversion rule:
// a numeric value the destination cannot represent exactly must error, never
// silently wrap, truncate, or change sign.
func TestAssign_CheckedNumericConversions(t *testing.T) {
	t.Parallel()

	t.Run("negative int64 -> uint64 errors", func(t *testing.T) {
		var n uint64
		if err := assign(&n, int64(-1)); err == nil {
			t.Fatalf("want error, got n=%d", n)
		}
	})
	t.Run("int64 overflow -> int8 errors", func(t *testing.T) {
		var n int8
		if err := assign(&n, int64(300)); err == nil {
			t.Fatalf("want error, got n=%d", n)
		}
	})
	t.Run("int64 max -> int32 errors", func(t *testing.T) {
		var n int32
		if err := assign(&n, int64(math.MaxInt64)); err == nil {
			t.Fatalf("want error, got n=%d", n)
		}
	})
	t.Run("float64 fraction -> int errors", func(t *testing.T) {
		var n int
		if err := assign(&n, 7.5); err == nil {
			t.Fatalf("want error, got n=%d", n)
		}
	})
	t.Run("float64 integral -> int ok", func(t *testing.T) {
		var n int
		if err := assign(&n, 7.0); err != nil || n != 7 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
	t.Run("float64 NaN -> int errors", func(t *testing.T) {
		var n int
		if err := assign(&n, math.NaN()); err == nil {
			t.Fatal("want error for NaN")
		}
	})
	t.Run("float64 +Inf -> int errors", func(t *testing.T) {
		var n int
		if err := assign(&n, math.Inf(1)); err == nil {
			t.Fatal("want error for +Inf")
		}
	})
	t.Run("float64 overflow -> float32 errors", func(t *testing.T) {
		var f float32
		if err := assign(&f, math.MaxFloat64); err == nil {
			t.Fatalf("want error, got f=%v", f)
		}
	})
	t.Run("float64 negative -> uint errors", func(t *testing.T) {
		var n uint
		if err := assign(&n, -2.0); err == nil {
			t.Fatalf("want error, got n=%d", n)
		}
	})
	t.Run("float64 huge -> int64 errors", func(t *testing.T) {
		var n int64
		// 2^63 is representable as float64 but overflows int64.
		if err := assign(&n, math.Ldexp(1, 63)); err == nil {
			t.Fatalf("want error, got n=%d", n)
		}
	})
	t.Run("int64 -> float64 ok", func(t *testing.T) {
		var f float64
		if err := assign(&f, int64(42)); err != nil || f != 42 {
			t.Fatalf("f=%v err=%v", f, err)
		}
	})
	t.Run("float64 integral -> uint ok", func(t *testing.T) {
		var n uint
		if err := assign(&n, 9.0); err != nil || n != 9 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
}

// TestScan_CountMismatch pins the count-mismatch refusal: destination /
// column count mismatches must error instead of silently zero-filling or
// dropping columns.
func TestScan_CountMismatch(t *testing.T) {
	t.Parallel()

	rows := &bqRows{current: []gcpbq.Value{int64(1), "a", true}}

	t.Run("too few destinations", func(t *testing.T) {
		var n int64
		var s string
		if err := rows.Scan(&n, &s); err == nil {
			t.Fatal("want error for 2 destinations over 3 columns")
		}
	})
	t.Run("too many destinations", func(t *testing.T) {
		var n int64
		var s string
		var b, extra bool
		if err := rows.Scan(&n, &s, &b, &extra); err == nil {
			t.Fatal("want error for 4 destinations over 3 columns")
		}
	})
	t.Run("exact count scans", func(t *testing.T) {
		var n int64
		var s string
		var b bool
		if err := rows.Scan(&n, &s, &b); err != nil {
			t.Fatal(err)
		}
		if n != 1 || s != "a" || !b {
			t.Fatalf("scanned %d %q %v", n, s, b)
		}
	})
}
