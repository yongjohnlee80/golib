// Package record is the debug recorder's file format: the Record, its Stage
// vocabulary and the JSONL writer, with no dependency on the windowing
// toolkit. A terminal build records the same file a windowed one does — the
// tui's own trace maps into it too — without linking Gio or needing cgo, which
// is why the recorder's types live here and package gui re-exports them.
//
// Its one capability is Recorder: anything that takes a Record without blocking the window.
// JSONLRecorder is golib's, one JSON object per line.
package record

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Stage is where along the input path a record was taken.
type Stage string

const (
	// StageIn: one event arrived from the toolkit, before the backend applied it.
	StageIn Stage = "in"
	// StageIME: the IME buffer's state after an event was applied.
	StageIME Stage = "ime"
	// StageOut: the key events committed text was delivered to the App as.
	StageOut Stage = "out"
	// StageCmd: a command the backend sent back to the toolkit.
	StageCmd Stage = "cmd"
	// StageDraw: the preedit the frame drew at the caret.
	StageDraw Stage = "draw"
	// StageMeta: the recorder's own note about the recording, not the input path —
	// a writer marking the records it had to drop.
	StageMeta Stage = "meta"
	// StageFrame: where a frame's input began and ended. The replay segments on
	// these: delivery happens at frame ends, so without them a recording cannot
	// say when the input path delivered.
	StageFrame Stage = "frame"
	// StageTui: the tui's own trace, mapped into the same file by a host that
	// records one (a tui.TraceEvent per record, its Kind the trace kind's name).
	// The replay validates these and skips them: the tui side is not the input
	// path's to reproduce.
	StageTui Stage = "tui"
	// StagePointer: the pointer path, for a pointer-shape report: a pointer event as the window
	// took it, and each new frame's cursor areas, both in device pixels. The replay validates these
	// and skips them: the pointer is not the input path's to reproduce.
	StagePointer Stage = "pointer"
)

// Record is one entry of a recording.
type Record struct {
	Seq   uint64         // the record's number, from 0, in the order they were taken
	At    time.Time      // when it was taken
	Stage Stage          // where along the input path
	Kind  string         // the event or command's type name, e.g. "TextEdit"
	Data  map[string]any // its fields; for StageIME: text, comp, sel, pieces
}

// Recorder receives the input path's records. Record must not block the window:
// implementations buffer and flush on their own terms.
type Recorder interface {
	Record(Record)
}

// JSONLRecorder writes records as one JSON object per line.
//
// Record never blocks the window goroutine: records go over a bounded channel to a
// writer goroutine that owns the buffered writer. Under sustained overload the channel
// can fill; the record is dropped then, and the writer notes the drop in the file — a
// {"Stage":"meta","Kind":"dropped"} line before the next record it writes, carrying the
// total dropped so far — so a recording never loses events silently. Close stops
// accepting records, waits for the writer to drain and flush, and returns the first
// error the recording hit.
//
// The writer also flushes whenever it has caught up with the channel: under load it
// batches, and once input pauses the file holds every record written so far, whole.
// A process that ends without Close (macOS's Quit ends it from the toolkit, never
// returning to the code that would close the recorder; a crash; a kill) loses only the
// records still in flight, not a buffer's worth cut mid-record.
type JSONLRecorder struct {
	rec     chan Record   // to the writer goroutine; never read after Close
	dropped atomic.Uint64 // records the window goroutine dropped
	done    chan struct{} // closed once the writer has drained and flushed
	err     error         // the writer's first error; stable once done is closed
	closeOn sync.Once     // Close drains once
}

// NewJSONLRecorder records to w, one JSON object per line.
func NewJSONLRecorder(w io.Writer) *JSONLRecorder {
	r := &JSONLRecorder{rec: make(chan Record, 4096), done: make(chan struct{})}
	go r.write(w)
	return r
}

// Record hands one record to the writer goroutine. It never blocks: when the channel
// is full the record is dropped and counted, and the writer notes the drop in the file
// before the next record it writes.
func (r *JSONLRecorder) Record(rec Record) {
	select {
	case r.rec <- rec:
	default:
		r.dropped.Add(1)
	}
}

// Dropped is how many records were dropped under load: the file notes them, and a
// caller reporting on the recording says so with this.
func (r *JSONLRecorder) Dropped() uint64 { return r.dropped.Load() }

// Close stops accepting records, waits for the writer to drain and flush, and returns
// the first error the recording hit. It is safe to call more than once.
func (r *JSONLRecorder) Close() error {
	r.closeOn.Do(func() {
		close(r.rec)
		<-r.done
	})
	return r.err
}

// write drains the channel into the buffered writer, on its own goroutine.
func (r *JSONLRecorder) write(w io.Writer) {
	defer close(r.done)
	bw := bufio.NewWriterSize(w, 1<<16)
	var noted uint64
	for rec := range r.rec {
		if n := r.dropped.Load(); n > noted {
			noted = n
			r.writeRec(bw, Record{Stage: StageMeta, Kind: "dropped", Data: map[string]any{"n": n}})
		}
		r.writeRec(bw, rec)
		if len(r.rec) == 0 && r.err == nil {
			r.setErr(bw.Flush()) // caught up: what is written so far reaches the file
		}
	}
	// Drops whose following record never came are noted too.
	if n := r.dropped.Load(); n > noted {
		r.writeRec(bw, Record{Stage: StageMeta, Kind: "dropped", Data: map[string]any{"n": n}})
	}
	r.setErr(bw.Flush())
}

// writeRec buffers one record as a JSON line. The first error ends the recording.
func (r *JSONLRecorder) writeRec(bw *bufio.Writer, rec Record) {
	if r.err != nil {
		return
	}
	b, err := json.Marshal(rec)
	if err == nil {
		_, err = bw.Write(append(b, '\n'))
	}
	r.setErr(err)
}

// setErr keeps the first error the recording hit; marshal, write and flush errors
// alike report through Close, and a later success never hides an earlier failure.
func (r *JSONLRecorder) setErr(err error) {
	if err != nil && r.err == nil {
		r.err = err
	}
}
