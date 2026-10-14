package record

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// notifyingWriter is the file a recording lands in, read by the test while the writer
// goroutine writes it: each write is announced, so the test waits on the write itself.
type notifyingWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	wrote chan struct{}
}

func newNotifyingWriter() *notifyingWriter {
	return &notifyingWriter{wrote: make(chan struct{}, 1)}
}

func (w *notifyingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buf.Write(p)
	w.mu.Unlock()
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return n, err
}

func (w *notifyingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// A process can end without Close: macOS's Quit ends it from the toolkit, never
// returning to the code that closes the recorder. What the recording took before input
// paused must already be in the file then, every line whole: before, all of it sat in
// the writer's buffer until 64 KiB filled, and the file ended mid-record.
func TestRecordsReachTheFileOnceTheWriterCatchesUp(t *testing.T) {
	t.Parallel()
	w := newNotifyingWriter()
	r := NewJSONLRecorder(w)
	defer r.Close()
	for i := range 3 {
		r.Record(Record{Seq: uint64(i), Stage: StageIn, Kind: "TextEdit", Data: map[string]any{"text": "한"}})
	}

	deadline := time.After(5 * time.Second)
	for {
		got := w.String()
		if strings.Count(got, "\n") == 3 && strings.HasSuffix(got, "\n") {
			for i, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
				var rec Record
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					t.Fatalf("line %d is not a whole record: %v: %q", i, err, line)
				}
				if rec.Seq != uint64(i) {
					t.Fatalf("line %d holds record %d, want %d", i, rec.Seq, i)
				}
			}
			return
		}
		select {
		case <-w.wrote:
		case <-deadline:
			t.Fatalf("with input paused and no Close, the file holds %q; want the 3 records, each line whole", got)
		}
	}
}
