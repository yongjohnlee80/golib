package gui

import (
	"io"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/gui/record"
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
//
// The recorder's own types live in [record], which imports no windowing
// toolkit, so a terminal build records the same file without linking Gio; the
// aliases below are gui's names for them.

// Stage is where along the input path a record was taken.
type Stage = record.Stage

// Record is one entry of a recording.
type Record = record.Record

// Recorder receives the input path's records. Record must not block the window:
// implementations buffer and flush on their own terms.
type Recorder = record.Recorder

// JSONLRecorder writes records as one JSON object per line, buffered through a
// writer goroutine; see [record.JSONLRecorder].
type JSONLRecorder = record.JSONLRecorder

// NewJSONLRecorder records to w, one JSON object per line.
func NewJSONLRecorder(w io.Writer) *JSONLRecorder { return record.NewJSONLRecorder(w) }

const (
	// StageIn: one event arrived from the toolkit, before the backend applied it.
	StageIn = record.StageIn
	// StageIME: the IME buffer's state after an event was applied.
	StageIME = record.StageIME
	// StageOut: the key events committed text was delivered to the App as.
	StageOut = record.StageOut
	// StageCmd: a command the backend sent back to the toolkit.
	StageCmd = record.StageCmd
	// StageDraw: the preedit the frame drew at the caret.
	StageDraw = record.StageDraw
	// StageMeta: the recorder's own note about the recording, not the input path —
	// a writer marking the records it had to drop.
	StageMeta = record.StageMeta
	// StageFrame: where a frame's input began and ended. The replay segments on
	// these: delivery happens at frame ends, so without them a recording cannot
	// say when the input path delivered.
	StageFrame = record.StageFrame
	// StageTui: the tui's own trace, mapped into the same file by a host that
	// records one. The replay validates these and skips them.
	StageTui = record.StageTui
	// StagePointer: a pointer event as the window took it, and each new frame's cursor areas.
	StagePointer = record.StagePointer
)

// inputRecorder is the input path's recorder (nil records nothing) and the next record's number.
// Both belong to the Gio goroutine: readInput, deliverIME, placeCaret and drawPreedit all run
// there. The recorder is closed once: by that goroutine as the window finishes (finishRun), or by
// Stop when the window never ran. The close's error is kept for Stop to report.
type inputRecorder struct {
	r    Recorder
	seq  uint64
	once sync.Once
	err  error
}

// on reports whether a recorder is installed. The hooks build their data inside their own on()
// guard, so the path with no recorder allocates nothing.
func (r *inputRecorder) on() bool { return r.r != nil }

// record numbers and hands one record to the installed recorder.
func (r *inputRecorder) record(stage Stage, kind string, data map[string]any) {
	rec := Record{Seq: r.seq, At: time.Now(), Stage: stage, Kind: kind, Data: data}
	r.seq++
	r.r.Record(rec)
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
	case KeyboardHide:
		return "KeyboardHide", map[string]any{}
	case FocusTake:
		return "FocusTake", map[string]any{}
	}
	return "unknown", nil
}

// imeFields flattens the IME buffer's state for a record.
func imeFields(s *imeState) map[string]any {
	pieces := make([]map[string]any, len(s.pieces))
	for i, p := range s.pieces {
		pieces[i] = map[string]any{"from": s.base + p.from, "text": string(p.runs), "folded": p.folded}
	}
	return map[string]any{
		"text":   string(s.text),
		"comp":   map[string]any{"start": s.comp.Start, "end": s.comp.End},
		"sel":    map[string]any{"start": s.sel.Start, "end": s.sel.End},
		"pieces": pieces,
	}
}

// close closes the recorder exactly once, from the only goroutine that records into it, and keeps
// the error for Stop to report.
func (r *inputRecorder) close() {
	if r.r == nil {
		return
	}
	r.once.Do(func() {
		if c, ok := r.r.(io.Closer); ok {
			r.err = c.Close()
		}
	})
}

// finishRun is run's tail: with the window goroutine's recording finished, the
// recorder is closed by its only writer, and only then is the goroutine's
// completion signalled for Stop to wait on.
func (b *Backend) finishRun() {
	b.rec.close()
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
