package ingestor

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"testing"
)

type jsonRow struct {
	Name  string
	Count int
}

func TestJSON_FlushWritesRemainderAndReturnsRows(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	j := NewJSON[jsonRow]("orders", 100, WithOpener(sink.open))

	if err := j.Commit(jsonRow{"a", 1}, jsonRow{"b", 2}); err != nil {
		t.Fatal(err)
	}
	rows, err := j.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 flushed rows, got %d", len(rows))
	}

	names := sink.names()
	if len(names) != 1 {
		t.Fatalf("expected 1 file, got %v", names)
	}
	var decoded []jsonRow
	if err := json.Unmarshal([]byte(sink.content(names[0])), &decoded); err != nil {
		t.Fatalf("file is not valid JSON: %v", err)
	}
	if len(decoded) != 2 || decoded[0] != (jsonRow{"a", 1}) || decoded[1] != (jsonRow{"b", 2}) {
		t.Fatalf("unexpected decoded rows: %+v", decoded)
	}
}

func TestJSON_CommitSpawnsBackgroundBatches(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	j := NewJSON[jsonRow]("batch", 3, WithOpener(sink.open))

	for i := range 7 {
		if err := j.Commit(jsonRow{Name: fmt.Sprintf("r%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := j.Flush()
	if err != nil {
		t.Fatal(err)
	}
	// 7 committed = 2 background batches of 3 + 1 remainder returned by Flush.
	if len(rows) != 1 {
		t.Fatalf("expected 1 remainder row, got %d", len(rows))
	}
	if names := sink.names(); len(names) != 3 {
		t.Fatalf("expected 3 files, got %v", names)
	}
}

func TestJSON_FilenameFormat(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	j := NewJSON[jsonRow]("my data/set", 1, WithOpener(sink.open))

	if err := j.Commit(jsonRow{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Flush(); err != nil {
		t.Fatal(err)
	}
	names := sink.names()
	if len(names) != 1 {
		t.Fatalf("expected 1 file, got %v", names)
	}
	pat := regexp.MustCompile(`^my-data-set-\d+-\d{3}\.json$`)
	if !pat.MatchString(names[0]) {
		t.Fatalf("unexpected filename %q", names[0])
	}
}

func TestJSON_WriteErrorsAggregatedByFlush(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	sink.fail = errors.New("no space")
	j := NewJSON[jsonRow]("err", 1, WithOpener(sink.open))

	if err := j.Commit(jsonRow{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	_, err := j.Flush()
	var batchErr *BatchErrors
	if !errors.As(err, &batchErr) {
		t.Fatalf("expected *BatchErrors, got %v", err)
	}
}

func TestJSON_ConcurrentCommitAndFlush(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	j := NewJSON[jsonRow]("race", 4, WithOpener(sink.open))

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			_ = j.Commit(jsonRow{Name: fmt.Sprintf("r%d", i), Count: i})
		})
		if i%4 == 0 {
			wg.Go(func() {
				_, _ = j.Flush()
			})
		}
	}
	wg.Wait()
	if _, err := j.Flush(); err != nil {
		t.Fatal(err)
	}

	total := 0
	for _, name := range sink.names() {
		var decoded []jsonRow
		if err := json.Unmarshal([]byte(sink.content(name)), &decoded); err != nil {
			t.Fatalf("file %s is not valid JSON: %v", name, err)
		}
		total += len(decoded)
	}
	if total != 20 {
		t.Fatalf("expected 20 rows across all files, got %d", total)
	}
}
