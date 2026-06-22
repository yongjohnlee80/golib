package bigquery

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

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
	for name, got := range map[string]bool{
		"SupportsReturning":    d.SupportsReturning(),
		"SupportsTransactions": d.SupportsTransactions(),
		"SupportsUpsert":       d.SupportsUpsert(),
		"SupportsLastInsertID": d.SupportsLastInsertID(),
		"CopySupported":        d.CopySupported(),
		"TwoPhaseSupported":    d.TwoPhaseSupported(),
	} {
		if got {
			t.Errorf("%s() = true, want false (no-transaction OLAP profile)", name)
		}
	}
	if s := d.BuildUpsertSuffix([]string{"id"}, []string{"name"}); s != "" {
		t.Errorf("BuildUpsertSuffix = %q, want empty", s)
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
