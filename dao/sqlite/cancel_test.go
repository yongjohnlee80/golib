package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"
)

// endless is a statement whose first step never ends: a recursive count with no bound, and no row
// until it has.
const endless = `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT x FROM c WHERE x < 0`

// openCancelDB is one in-memory database on one connection, through database/sql as a consumer
// reaches it.
func openCancelDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// A cancelled context ends a statement whatever moment the cancel lands, before its first step
// among them: modernc.org/sqlite before v1.61.0 interrupted once, and SQLite drops an interrupt
// that lands before a statement starts, so the statement ran on (here, forever). From v1.61.0 the
// driver checks the context as the statement steps. Each trial cancels at a different moment
// around the statement's start; each must end within the bound.
func TestACancelledStatementEndsWheneverTheCancelLands(t *testing.T) {
	db := openCancelDB(t)
	for trial := range 200 {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(time.Duration(trial%20) * 50 * time.Microsecond) // 0 to ~1ms: before, at and after the start
			cancel()
		}()
		done := make(chan error, 1)
		go func() {
			rows, err := db.QueryContext(ctx, endless)
			if err == nil {
				for rows.Next() {
				}
				err = rows.Err()
				_ = rows.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("trial %d: the endless statement ended with no error", trial)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("trial %d: the statement still runs 5s after its context was cancelled: the cancel was lost", trial)
		}
		cancel()
	}
}

// laterStep is a statement whose first row comes at once and whose next takes a long step: a
// bounded count, the first row at its start and the next at its end.
const laterStep = `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 20000000) SELECT x FROM c WHERE x = 1 OR x = 20000000`

// A cancel during a later step of a query, inside rows.Next after the first row, ends it.
// modernc.org/sqlite v1.61.1 watches the context only while the query's first step runs (its
// progress handler and interrupt are armed for QueryContext's call, and released when it returns),
// and database/sql closes the rows only between Next calls: so a slow later step runs to its end
// whatever the context. This is the report for upstream; it fails on v1.61.1, so it runs only when
// asked: GOLIB_SQLITE_LATER_STEP=1.
func TestACancelDuringALaterStepEndsIt(t *testing.T) {
	if os.Getenv("GOLIB_SQLITE_LATER_STEP") != "1" {
		t.Skip("modernc.org/sqlite v1.61.1 doesn't interrupt a later step (the upstream report); GOLIB_SQLITE_LATER_STEP=1 runs it")
	}
	db := openCancelDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rows, err := db.QueryContext(ctx, laterStep)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no first row: %v", rows.Err())
	}
	done := make(chan struct{})
	go func() {
		rows.Next() // the long step
		close(done)
	}()
	time.Sleep(100 * time.Millisecond) // inside the step
	cancelled := time.Now()
	cancel()
	<-done
	took := time.Since(cancelled)
	if took > time.Second {
		t.Errorf("rows.Next returned %v after its context was cancelled: the later step wasn't interrupted (want under 1s)", took.Round(time.Millisecond))
	}
	if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
		t.Logf("rows.Err: %v", err)
	}
}
