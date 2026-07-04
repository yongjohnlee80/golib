package ingestor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// memSink collects batch files written through WithOpener in memory.
type memSink struct {
	mu    sync.Mutex
	files map[string]*bytes.Buffer
	fail  error // when set, open fails with this error
}

func newMemSink() *memSink {
	return &memSink{files: map[string]*bytes.Buffer{}}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func (s *memSink) open(name string) (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	buf := &bytes.Buffer{}
	s.files[name] = buf
	return nopWriteCloser{buf}, nil
}

func (s *memSink) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.files))
	for n := range s.files {
		names = append(names, n)
	}
	return names
}

func (s *memSink) content(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.files[name]; ok {
		return b.String()
	}
	return ""
}

type csvRow struct {
	Name  string
	Count int
	note  string // unexported: must not appear in header or records
}

func TestCSV_FlushWritesRemainderAndReturnsRows(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	c := NewCSV[csvRow]("orders", WithBatchSize(100), WithOpener(sink.open))

	if err := c.Commit(t.Context(), csvRow{"a", 1, "x"}, csvRow{"b", 2, "y"}); err != nil {
		t.Fatal(err)
	}
	rows, err := c.Flush(t.Context())
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
	got := sink.content(names[0])
	want := "Name,Count\na,1\nb,2\n"
	if got != want {
		t.Fatalf("unexpected CSV content:\n got: %q\nwant: %q", got, want)
	}
}

func TestCSV_CommitSpawnsBackgroundBatches(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	c := NewCSV[csvRow]("batch", WithBatchSize(2), WithOpener(sink.open))

	for i := range 5 {
		if err := c.Commit(t.Context(), csvRow{Name: fmt.Sprintf("r%d", i), Count: i}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := c.Flush(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 5 committed = 2 background batches of 2 + 1 remainder returned by Flush.
	if len(rows) != 1 {
		t.Fatalf("expected 1 remainder row, got %d", len(rows))
	}
	if names := sink.names(); len(names) != 3 {
		t.Fatalf("expected 3 files (2 background + 1 flush), got %v", names)
	}
	if c.Total() != 5 {
		t.Fatalf("expected total 5, got %d", c.Total())
	}
}

func TestCSV_FilenameFormat(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	c := NewCSV[csvRow]("my orders/2026", WithBatchSize(1), WithOpener(sink.open))

	if err := c.Commit(t.Context(), csvRow{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	names := sink.names()
	if len(names) != 1 {
		t.Fatalf("expected 1 file, got %v", names)
	}
	// Sanitized description, unix timestamp, zero-padded counter, no spaces.
	pat := regexp.MustCompile(`^my-orders-2026-\d+-\d{3}\.csv$`)
	if !pat.MatchString(names[0]) {
		t.Fatalf("unexpected filename %q", names[0])
	}
	if strings.ContainsAny(names[0], " ()") {
		t.Fatalf("filename contains unsafe characters: %q", names[0])
	}
}

func TestCSV_WriteErrorsAggregatedByFlush(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	sink.fail = errors.New("disk full")
	c := NewCSV[csvRow]("err", WithBatchSize(1), WithOpener(sink.open))

	if err := c.Commit(t.Context(), csvRow{Name: "a"}, csvRow{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	_, err := c.Flush(t.Context())
	var batchErr *BatchErrors
	if !errors.As(err, &batchErr) {
		t.Fatalf("expected *BatchErrors, got %v", err)
	}
	if !errors.Is(err, sink.fail) {
		t.Fatal("expected wrapped disk-full error in chain")
	}
	// Errors were drained: a second flush with no data reports none.
	if _, err := c.Flush(t.Context()); err != nil {
		t.Fatalf("expected drained errors on second flush, got %v", err)
	}
}

func TestCSV_WithDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := NewCSV[csvRow]("dirtest", WithBatchSize(10), WithDir(dir))

	if err := c.Commit(t.Context(), csvRow{Name: "a", Count: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "dirtest-*.csv"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected 1 csv file in %s, got %v (err %v)", dir, matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Name,Count\n") {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestCSV_ConcurrentCommitAndFlush(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	c := NewCSV[csvRow]("race", WithBatchSize(4), WithOpener(sink.open))

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			_ = c.Commit(t.Context(), csvRow{Name: fmt.Sprintf("r%d", i), Count: i})
		})
		if i%5 == 0 {
			wg.Go(func() {
				_, _ = c.Flush(t.Context())
			})
		}
	}
	wg.Wait()
	if _, err := c.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Every committed row must land in exactly one file: total row count
	// across all files (excluding headers) == 20.
	total := 0
	for _, name := range sink.names() {
		lines := strings.Split(strings.TrimSpace(sink.content(name)), "\n")
		total += len(lines) - 1 // minus header
	}
	if total != 20 {
		t.Fatalf("expected 20 rows across all files, got %d", total)
	}
	if c.Total() != 20 {
		t.Fatalf("expected total 20, got %d", c.Total())
	}
}

func TestCSVHeaderRow_SkipsUnexported(t *testing.T) {
	t.Parallel()
	header, err := CSVHeaderRow(csvRow{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Name", "Count"}
	if len(header) != len(want) || header[0] != want[0] || header[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, header)
	}
}

func TestCSV_ContextCancelled(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	c := NewCSV[csvRow]("ctx", WithOpener(sink.open))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Commit(ctx, csvRow{Name: "a"}); err == nil {
		t.Fatal("expected error from cancelled Commit")
	}
	if _, err := c.Flush(ctx); err == nil {
		t.Fatal("expected error from cancelled Flush")
	}
	if c.Total() != 0 {
		t.Errorf("cancelled Commit must not buffer, total = %d", c.Total())
	}
}

func TestCSV_CloseWritesRemainder(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	c := NewCSV[csvRow]("close", WithOpener(sink.open))

	if err := c.Commit(t.Context(), csvRow{Name: "a", Count: 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if names := sink.names(); len(names) != 1 {
		t.Fatalf("Close must write the remainder, files = %v", names)
	}
}

type taggedRow struct {
	Name   string `csv:"full_name"`
	Secret string `csv:"-"`
	Plain  int
}

func TestCSV_TagOverridesAndOmits(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	c := NewCSV[taggedRow]("tags", WithOpener(sink.open))

	if err := c.Commit(t.Context(), taggedRow{Name: "jo", Secret: "hide", Plain: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := sink.content(sink.names()[0])
	want := "full_name,Plain\njo,2\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if strings.Contains(got, "hide") {
		t.Error("csv:\"-\" field leaked into output")
	}
}

func TestCSV_MaxWritersBoundsConcurrency(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	inflight, peak := 0, 0
	gate := make(chan struct{})
	opener := func(name string) (io.WriteCloser, error) {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		<-gate // hold every writer open until released
		mu.Lock()
		inflight--
		mu.Unlock()
		return nopWriteCloser{&bytes.Buffer{}}, nil
	}

	c := NewCSV[csvRow]("bounded", WithBatchSize(1), WithMaxWriters(2), WithOpener(opener))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 6 {
			_ = c.Commit(context.Background(), csvRow{Name: fmt.Sprintf("r%d", i)})
		}
	}()

	// Let writers saturate, then release them all.
	time.Sleep(100 * time.Millisecond)
	close(gate)
	<-done
	if _, err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > 2 {
		t.Errorf("peak concurrent writers = %d, want <= 2", peak)
	}
	if peak == 0 {
		t.Error("no background writers observed")
	}
}
