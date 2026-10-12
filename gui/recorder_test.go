package gui

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"

	"github.com/yongjohnlee80/golib/tui"
)

// recMatches asserts one record's Seq, Stage, Kind and Data (At is wall-clock noise).
func recMatches(t *testing.T, got Record, seq uint64, stage Stage, kind string, data map[string]any) {
	t.Helper()
	if got.Seq != seq || got.Stage != stage || got.Kind != kind {
		t.Fatalf("record %d: seq %d stage %q kind %q; want %d %q %q", got.Seq, got.Seq, got.Stage, got.Kind, seq, stage, kind)
	}
	if !reflect.DeepEqual(got.Data, data) {
		t.Fatalf("record %d (%s %s): data %v; want %v", seq, stage, kind, got.Data, data)
	}
}

// A scripted Hangul composition, driven through readInput with a real Gio router as the
// input method, must come out of the recorder as the input path in records: each platform
// event in, the IME buffer's state after it, the delivered keys out, and the two resets
// sent back to the toolkit. Removing any hook fails this cell.
func TestARecordedHangulCompositionShowsTheInputPathInRecords(t *testing.T) {
	ring := newRingRecorder(64)
	r := new(input.Router)
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	// Take focus as focusedBackend does: the router answers the focus request with its
	// own focus events, which are not this cell's subject. Recording starts after them.
	b.readInput(r.Source(), tag, metrics{})
	b.readInput(r.Source(), tag, metrics{})
	b.q.queue = nil
	b.rec = ring
	r.Queue(
		key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "ㅎ"},
		key.SnippetEvent(key.Range{Start: 0, End: 1}),
		key.CompositionEvent{Start: 0, End: 1},
		key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "한"},
		key.CompositionEvent{Start: -1, End: -1},
	)
	b.readInput(r.Source(), tag, metrics{})
	got := ring.records()
	if len(got) != 16 {
		for i, rec := range got {
			t.Logf("record %d: %+v", i, rec)
		}
		t.Fatalf("the composition was recorded as %d records; want 16 (frame begin, in/ime per event, the snippet reply, out, two cmds, frame end)", len(got))
	}
	recMatches(t, got[0], 0, StageFrame, "begin", map[string]any{"n": 3})
	recMatches(t, got[1], 1, StageIn, "TextEdit", map[string]any{"start": 0, "end": 0, "text": "ㅎ"})
	recMatches(t, got[2], 2, StageIME, "imeState", map[string]any{
		"text": "ㅎ", "comp": map[string]any{"start": 0, "end": 0}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "ㅎ", "folded": false}},
	})
	recMatches(t, got[3], 3, StageIn, "SnippetAsk", map[string]any{"start": 0, "end": 1})
	// The answer to the snippet ask is a cmd record: what the toolkit was told the editor
	// holds, which is the handshake the macOS bug lived in.
	recMatches(t, got[4], 4, StageCmd, "SnippetReply", map[string]any{"start": 0, "end": 1, "text": "ㅎ"})
	recMatches(t, got[5], 5, StageIME, "imeState", map[string]any{
		"text": "ㅎ", "comp": map[string]any{"start": 0, "end": 0}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "ㅎ", "folded": false}},
	})
	recMatches(t, got[6], 6, StageIn, "Composition", map[string]any{"start": 0, "end": 1})
	recMatches(t, got[7], 7, StageIME, "imeState", map[string]any{
		"text": "ㅎ", "comp": map[string]any{"start": 0, "end": 1}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "ㅎ", "folded": false}},
	})
	recMatches(t, got[8], 8, StageIn, "TextEdit", map[string]any{"start": 0, "end": 1, "text": "한"})
	recMatches(t, got[9], 9, StageIME, "imeState", map[string]any{
		"text": "한", "comp": map[string]any{"start": 0, "end": 1}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "한", "folded": true}},
	})
	recMatches(t, got[10], 10, StageIn, "Composition", map[string]any{"start": -1, "end": -1})
	recMatches(t, got[11], 11, StageIME, "imeState", map[string]any{
		"text": "한", "comp": map[string]any{"start": -1, "end": -1}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "한", "folded": true}},
	})
	recMatches(t, got[12], 12, StageOut, "KeyEvent", map[string]any{
		"events": []tui.KeyEvent{{Kind: tui.KeyPress, Code: '한', Text: "한"}},
	})
	recMatches(t, got[13], 13, StageCmd, "SnippetReply", map[string]any{"start": 0, "end": 0, "text": ""})
	recMatches(t, got[14], 14, StageCmd, "SelectionTell", map[string]any{
		"start": 0, "end": 0,
		"caret": map[string]any{"x": float32(0), "y": float32(0), "ascent": float32(0), "descent": float32(0)},
	})
	recMatches(t, got[15], 15, StageFrame, "end", map[string]any{"n": 3})
}

// The recording a JSONL recorder writes parses back line by line into the records that
// were taken, so a recording made on one machine can be read on another.
func TestTheJSONLRecordingParsesBack(t *testing.T) {
	var buf bytes.Buffer
	rec := NewJSONLRecorder(&buf)
	r := new(input.Router)
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	b.readInput(r.Source(), tag, metrics{})
	b.readInput(r.Source(), tag, metrics{}) // the router's focus events are not this cell's subject
	b.rec = rec
	r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "a"})
	b.readInput(r.Source(), tag, metrics{})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("the recording holds %d lines; want 7 (frame begin, in, ime, out, two cmds, frame end)", len(lines))
	}
	var recs []Record
	for _, line := range lines {
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a recording line is not one JSON record: %v\n%s", err, line)
		}
		recs = append(recs, rec)
	}
	if recs[0].Stage != StageFrame || recs[0].Kind != "begin" {
		t.Errorf("line 1 = %s %s; want the frame's begin", recs[0].Stage, recs[0].Kind)
	}
	if recs[1].Stage != StageIn || recs[1].Kind != "TextEdit" || recs[1].Data["text"] != "a" {
		t.Errorf("line 2 = %s %s %v; want the TextEdit in", recs[1].Stage, recs[1].Kind, recs[1].Data)
	}
	if recs[2].Stage != StageIME || recs[2].Data["text"] != "a" {
		t.Errorf("line 3 = %s %v; want the imeState after it", recs[2].Stage, recs[2].Data)
	}
	if recs[3].Stage != StageOut {
		t.Errorf("line 4 = %s; want the out", recs[3].Stage)
	}
	events, ok := recs[3].Data["events"].([]any)
	if !ok || len(events) != 1 || events[0].(map[string]any)["Text"] != "a" {
		t.Errorf("line 4's events = %v; want the one key event of \"a\"", recs[3].Data["events"])
	}
	if recs[4].Stage != StageCmd || recs[4].Kind != "SnippetReply" {
		t.Errorf("line 5 = %s %s; want the snippet reset", recs[4].Stage, recs[4].Kind)
	}
	if recs[5].Stage != StageCmd || recs[5].Kind != "SelectionTell" {
		t.Errorf("line 6 = %s %s; want the selection reset", recs[5].Stage, recs[5].Kind)
	}
	if recs[6].Stage != StageFrame || recs[6].Kind != "end" {
		t.Errorf("line 7 = %s %s; want the frame's end", recs[6].Stage, recs[6].Kind)
	}
	for i, rec := range recs {
		if rec.Seq != uint64(i) {
			t.Errorf("line %d carries seq %d; the lines are the records in order", i+1, rec.Seq)
		}
		if rec.At.IsZero() {
			t.Errorf("line %d carries no time", i+1)
		}
	}
}

func TestTheRingRecorderKeepsTheLastRecords(t *testing.T) {
	ring := newRingRecorder(3)
	for i := range 8 {
		ring.Record(Record{Seq: uint64(i), Kind: "r"})
	}
	got := ring.records()
	want := []uint64{5, 6, 7}
	if len(got) != len(want) {
		t.Fatalf("the ring holds %d records; want the last 3", len(got))
	}
	for i, seq := range want {
		if got[i].Seq != seq {
			t.Errorf("ring[%d] is seq %d; want %d (oldest first)", i, got[i].Seq, seq)
		}
	}
	empty := newRingRecorder(0)
	empty.Record(Record{})
	if got := empty.records(); len(got) != 0 {
		t.Fatalf("a zero-capacity ring held %d records", len(got))
	}
}

// With no recorder, the input path allocates nothing for the recorder's sake.
// The six allocations readInput makes per call are its own, and predate the
// recorder: they are the variadic filter slice it hands the event source, and
// the filters boxed into it. Every recorder operation is behind b.rec != nil,
// and none of them runs here — the same probe against origin/main, which has
// no recorder at all, measures the same six.
func TestTheUnrecordedInputPathAllocatesNothing(t *testing.T) {
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	src := &recordingSource{}
	b.readInput(src, tag, metrics{}) // warm up: takes focus, which the fake source absorbs
	if allocs := testing.AllocsPerRun(100, func() {
		b.readInput(src, tag, metrics{})
	}); allocs > 6 {
		t.Fatalf("readInput with no recorder allocated %v per call; the recorder must add nothing to the six it already made before it existed", allocs)
	}
}

// The focus handshake is recorded too: the prologue's commands — taking the focus and
// opening the text input, which on Wayland is what lets an input method attach at all —
// are cmd records, in the order they were sent.
func TestTheFocusHandshakeIsRecorded(t *testing.T) {
	ring := newRingRecorder(8)
	b := &Backend{q: newEventQueue(), rec: ring}
	tag := &b.gio
	src := &recordingSource{}
	b.readInput(src, tag, metrics{})
	got := ring.records()
	if len(got) != 4 {
		for i, rec := range got {
			t.Logf("record %d: %+v", i, rec)
		}
		t.Fatalf("the focus handshake was recorded as %d records; want the frame's begin, the FocusTake and KeyboardShow cmds, and the frame's end", len(got))
	}
	recMatches(t, got[0], 0, StageFrame, "begin", map[string]any{"n": 1})
	recMatches(t, got[1], 1, StageCmd, "FocusTake", map[string]any{})
	recMatches(t, got[2], 2, StageCmd, "KeyboardShow", map[string]any{})
	recMatches(t, got[3], 3, StageFrame, "end", map[string]any{"n": 1})
}

// A recorder that panics if a Record arrives after its Close: if anything but the
// window goroutine closed the recorder, or closed it early, recording would panic.
type panicAfterCloseRecorder struct {
	closed atomic.Bool
}

func (p *panicAfterCloseRecorder) Record(Record) {
	if p.closed.Load() {
		panic("a record arrived after the recorder was closed")
	}
}

func (p *panicAfterCloseRecorder) Close() error {
	p.closed.Store(true)
	return nil
}

// Stop must not close the recorder itself, and must not return before the window
// goroutine has finished recording and closed it: the recorder's only writer
// closes it, last, in run's tail.
func TestStopWaitsForTheWindowGoroutineBeforeTheRecorderCloses(t *testing.T) {
	pr := &panicAfterCloseRecorder{}
	b := NewBackend(WithRecorder(pr))
	b.runLaunched.Store(true) // as Start leaves it: run's tail closes the recorder, Stop waits for it
	go b.q.forward()
	flowing := make(chan struct{})
	go func() { // the window goroutine: records flow while the App stops, then run's tail
		defer close(flowing)
		for i := range 100000 {
			b.record(StageIn, "TextEdit", map[string]any{"i": i})
		}
		b.finishRun()
	}()
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-flowing
	if !pr.closed.Load() {
		t.Fatal("Stop returned before the recorder was closed")
	}
}

// A second Stop must not close the recorder again: the close is once, and both Stops
// report its error.
type closingCounterRecorder struct {
	closes atomic.Int32
}

func (c *closingCounterRecorder) Record(Record) {}

func (c *closingCounterRecorder) Close() error {
	c.closes.Add(1)
	return nil
}

func TestASecondStopClosesTheRecorderOnce(t *testing.T) {
	pr := &closingCounterRecorder{}
	b := NewBackend(WithRecorder(pr))
	b.runLaunched.Store(true)
	go b.q.forward()
	b.finishRun() // the window goroutine's tail, as run defers it
	if err := b.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if n := pr.closes.Load(); n != 1 {
		t.Fatalf("the recorder was closed %d times; a second Stop must not close it again", n)
	}
}

// A stalled writer must not stall the window goroutine: records that do not fit the
// handoff channel are dropped, counted, and the writer notes the drop in the file
// before the next record it writes — a recording never loses events silently.
func TestABlockedWriterDropsRatherThanBlocks(t *testing.T) {
	release := make(chan struct{})
	w := &blockingWriter{release: release}
	rec := NewJSONLRecorder(w)
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := range 20000 {
			rec.Record(Record{Seq: uint64(i), Kind: "r"})
		}
	}()
	select {
	case <-sent: // recorded everything without the writer ever leaving its first flush
	case <-time.After(10 * time.Second):
		t.Fatal("Record blocked on a stalled writer")
	}
	if rec.dropped.Load() == 0 {
		t.Fatal("a stalled writer with a full handoff channel dropped nothing")
	}
	close(release)
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The drop notes are cumulative and never exceed the final total; drops keep
	// landing while the writer drains, so there can be several — the last carries
	// the total.
	var notes []uint64
	for _, line := range strings.Split(strings.TrimRight(w.buf.String(), "\n"), "\n") {
		if !strings.Contains(line, `"Kind":"dropped"`) {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a dropped line is not one JSON record: %v\n%s", err, line)
		}
		notes = append(notes, uint64(rec.Data["n"].(float64)))
	}
	if len(notes) == 0 {
		t.Fatalf("a stalled writer with a full handoff channel dropped %d records and noted none in the file", rec.dropped.Load())
	}
	for i, n := range notes {
		if i > 0 && n < notes[i-1] {
			t.Fatalf("drop notes %v are not cumulative", notes)
		}
		if n > rec.dropped.Load() {
			t.Fatalf("a drop note claims %d of %d dropped", n, rec.dropped.Load())
		}
	}
	if notes[len(notes)-1] != rec.dropped.Load() {
		t.Fatalf("the last drop note carries %d; want the %d dropped", notes[len(notes)-1], rec.dropped.Load())
	}
}

// blockingWriter releases nothing until its release channel closes, then keeps
// everything written to it.
type blockingWriter struct {
	release chan struct{}
	buf     bytes.Buffer
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.release
	return w.buf.Write(p)
}

// A backend stopped before it ever started must still close its recorder: no run
// goroutine ever will. The JSONL writer drains and flushes what was recorded, and a
// second Stop is harmless.
func TestAStopBeforeStartClosesTheRecorder(t *testing.T) {
	var buf bytes.Buffer
	rec := NewJSONLRecorder(&buf)
	b := NewBackend(WithRecorder(rec))
	b.record(StageIn, "TextEdit", map[string]any{"text": "a"})
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	// The recorder is closed and flushed by Stop alone — the writer goroutine has
	// exited, or Close could not have returned.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the stopped-before-start recording holds %d lines; want the one recorded event, flushed", len(lines))
	}
	var got Record
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil || got.Kind != "TextEdit" || got.Data["text"] != "a" {
		t.Fatalf("line 1 = %v (%v); want the TextEdit record", got, err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("the recorder's own Close after the backend closed it: %v", err)
	}
}

// A record that cannot be marshalled is the recording's first error, and a
// successful flush does not overwrite it: Close reports the marshal error, and the
// records before it still land in the file.
func TestAnUnmarshallableRecordIsTheRecordingsStickyError(t *testing.T) {
	var buf bytes.Buffer
	rec := NewJSONLRecorder(&buf)
	rec.Record(Record{Seq: 0, Kind: "good"})
	rec.Record(Record{Seq: 1, Kind: "bad", Data: map[string]any{"bad": make(chan int)}})
	rec.Record(Record{Seq: 2, Kind: "after"})
	if err := rec.Close(); err == nil {
		t.Fatal("Close returned nil; want the marshal error of the unmarshallable record")
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the recording holds %d lines; want the one good record before the error", len(lines))
	}
	var got Record
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil || got.Kind != "good" {
		t.Fatalf("line 1 = %v (%v); want the record before the error", got, err)
	}
}

func TestWithRecorderInstallsTheRecorder(t *testing.T) {
	ring := newRingRecorder(4)
	if b := NewBackend(WithRecorder(ring)); b.rec != Recorder(ring) {
		t.Fatal("WithRecorder's recorder did not reach the backend")
	}
	if b := NewBackend(); b.rec != nil {
		t.Fatal("a backend built with no WithRecorder must record nothing")
	}
}
