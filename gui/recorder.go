package gui

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// The debug recorder: a stream of records of the input path, platform event to
// drawn preedit, taken while the window runs. A recording is how an
// input-method bug on one machine is replayed on another: the records name
// gui's own contract types (TextEdit, Composition, SnippetReply...), never the
// toolkit's.
//
// A recorder is installed with [WithRecorder]. Without one every hook is behind
// a nil check and nothing is recorded or allocated. Records are all taken on
// the window's one input goroutine, so a Recorder needs no locking.

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

// record numbers and hands one record to the installed recorder. The hooks build
// their data inside their own b.rec != nil guard, so the path with no recorder
// allocates nothing.
func (b *Backend) record(stage Stage, kind string, data map[string]any) {
	rec := Record{Seq: b.recSeq, At: time.Now(), Stage: stage, Kind: kind, Data: data}
	b.recSeq++
	b.rec.Record(rec)
}

// eventFields flattens one InputEvent into its record's kind and fields.
func eventFields(ev InputEvent) (string, map[string]any) {
	switch e := ev.(type) {
	case KeyInput:
		return "KeyInput", map[string]any{"name": e.Name, "mods": e.Mods, "press": e.Press}
	case TextEdit:
		return "TextEdit", map[string]any{"start": e.Start, "end": e.End, "text": e.Text}
	case Composition:
		return "Composition", map[string]any{"start": e.Start, "end": e.End}
	case SelectionSet:
		return "SelectionSet", map[string]any{"start": e.Start, "end": e.End}
	case SnippetAsk:
		return "SnippetAsk", map[string]any{"start": e.Start, "end": e.End}
	case FocusChange:
		return "FocusChange", map[string]any{"focused": e.Focused}
	}
	return "unknown", nil
}

// cmdFields flattens one TextInputCmd into its record's kind and fields.
func cmdFields(cmd TextInputCmd) (string, map[string]any) {
	switch c := cmd.(type) {
	case SnippetReply:
		return "SnippetReply", map[string]any{"start": c.Start, "end": c.End, "text": c.Text}
	case SelectionTell:
		return "SelectionTell", map[string]any{
			"start": c.Start, "end": c.End,
			"caret": map[string]any{"x": c.Caret.X, "y": c.Caret.Y, "ascent": c.Caret.Ascent, "descent": c.Caret.Descent},
		}
	case KeyboardShow:
		return "KeyboardShow", map[string]any{}
	case FocusTake:
		return "FocusTake", map[string]any{}
	}
	return "unknown", nil
}

// imeFields flattens the IME buffer's state for a record.
func imeFields(s *imeState) map[string]any {
	pieces := make([]map[string]any, len(s.pieces))
	for i, p := range s.pieces {
		pieces[i] = map[string]any{"from": p.from, "text": string(p.runs), "folded": p.folded}
	}
	return map[string]any{
		"text":   string(s.text),
		"comp":   map[string]any{"start": s.comp.Start, "end": s.comp.End},
		"sel":    map[string]any{"start": s.sel.Start, "end": s.sel.End},
		"pieces": pieces,
	}
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

// closeRecorder closes the recorder exactly once, from the only goroutine that
// records into it, and keeps the error for Stop to report.
func (b *Backend) closeRecorder() {
	if b.rec == nil {
		return
	}
	b.closeRec.Do(func() {
		if c, ok := b.rec.(io.Closer); ok {
			b.recErr = c.Close()
		}
	})
}

// finishRun is run's tail: with the window goroutine's recording finished, the
// recorder is closed by its only writer, and only then is the goroutine's
// completion signalled for Stop to wait on.
func (b *Backend) finishRun() {
	b.closeRecorder()
	close(b.runDone)
}

// ringRecorder keeps the last records, overwriting the oldest, for tests.
type ringRecorder struct {
	rec []Record // the retained records; grows to capacity then wraps
	at  int      // the next write position once full; before that, len(rec)
}

// newRingRecorder keeps the last capacity records.
func newRingRecorder(capacity int) *ringRecorder {
	return &ringRecorder{rec: make([]Record, 0, capacity)}
}

// Record retains one record, overwriting the oldest once the ring is full.
func (r *ringRecorder) Record(rec Record) {
	if cap(r.rec) == 0 {
		return
	}
	if len(r.rec) < cap(r.rec) {
		r.rec = append(r.rec, rec)
		r.at = len(r.rec)
		return
	}
	r.rec[r.at%cap(r.rec)] = rec
	r.at++
}

// records returns what the ring holds, oldest first.
func (r *ringRecorder) records() []Record {
	if cap(r.rec) == 0 || len(r.rec) < cap(r.rec) {
		return append([]Record(nil), r.rec...)
	}
	n := cap(r.rec)
	out := make([]Record, 0, n)
	out = append(out, r.rec[r.at%n:]...)
	return append(out, r.rec[:r.at%n]...)
}
