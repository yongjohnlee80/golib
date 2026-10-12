package gui

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

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
		key.CompositionEvent{Start: 0, End: 1},
		key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "한"},
		key.CompositionEvent{Start: -1, End: -1},
	)
	b.readInput(r.Source(), tag, metrics{})
	got := ring.records()
	if len(got) != 11 {
		for i, rec := range got {
			t.Logf("record %d: %+v", i, rec)
		}
		t.Fatalf("the composition was recorded as %d records; want 11 (in/ime per event, out, two cmds)", len(got))
	}
	recMatches(t, got[0], 0, StageIn, "TextEdit", map[string]any{"start": 0, "end": 0, "text": "ㅎ"})
	recMatches(t, got[1], 1, StageIME, "imeState", map[string]any{
		"text": "ㅎ", "comp": map[string]any{"start": 0, "end": 0}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "ㅎ", "folded": false}},
	})
	recMatches(t, got[2], 2, StageIn, "Composition", map[string]any{"start": 0, "end": 1})
	recMatches(t, got[3], 3, StageIME, "imeState", map[string]any{
		"text": "ㅎ", "comp": map[string]any{"start": 0, "end": 1}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "ㅎ", "folded": false}},
	})
	recMatches(t, got[4], 4, StageIn, "TextEdit", map[string]any{"start": 0, "end": 1, "text": "한"})
	recMatches(t, got[5], 5, StageIME, "imeState", map[string]any{
		"text": "한", "comp": map[string]any{"start": 0, "end": 1}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "한", "folded": true}},
	})
	recMatches(t, got[6], 6, StageIn, "Composition", map[string]any{"start": -1, "end": -1})
	recMatches(t, got[7], 7, StageIME, "imeState", map[string]any{
		"text": "한", "comp": map[string]any{"start": -1, "end": -1}, "sel": map[string]any{"start": 0, "end": 0},
		"pieces": []map[string]any{{"from": 0, "text": "한", "folded": true}},
	})
	recMatches(t, got[8], 8, StageOut, "KeyEvent", map[string]any{
		"events": []tui.KeyEvent{{Kind: tui.KeyPress, Code: '한', Text: "한"}},
	})
	recMatches(t, got[9], 9, StageCmd, "SnippetReply", map[string]any{"start": 0, "end": 0, "text": ""})
	recMatches(t, got[10], 10, StageCmd, "SelectionTell", map[string]any{
		"start": 0, "end": 0,
		"caret": map[string]any{"x": float32(0), "y": float32(0), "ascent": float32(0), "descent": float32(0)},
	})
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
	if len(lines) != 5 {
		t.Fatalf("the recording holds %d lines; want 5 (in, ime, out, two cmds)", len(lines))
	}
	var recs []Record
	for _, line := range lines {
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a recording line is not one JSON record: %v\n%s", err, line)
		}
		recs = append(recs, rec)
	}
	if recs[0].Stage != StageIn || recs[0].Kind != "TextEdit" || recs[0].Data["text"] != "a" {
		t.Errorf("line 1 = %s %s %v; want the TextEdit in", recs[0].Stage, recs[0].Kind, recs[0].Data)
	}
	if recs[1].Stage != StageIME || recs[1].Data["text"] != "a" {
		t.Errorf("line 2 = %s %v; want the imeState after it", recs[1].Stage, recs[1].Data)
	}
	if recs[2].Stage != StageOut {
		t.Errorf("line 3 = %s; want the out", recs[2].Stage)
	}
	events, ok := recs[2].Data["events"].([]any)
	if !ok || len(events) != 1 || events[0].(map[string]any)["Text"] != "a" {
		t.Errorf("line 3's events = %v; want the one key event of \"a\"", recs[2].Data["events"])
	}
	if recs[3].Stage != StageCmd || recs[3].Kind != "SnippetReply" {
		t.Errorf("line 4 = %s %s; want the snippet reset", recs[3].Stage, recs[3].Kind)
	}
	if recs[4].Stage != StageCmd || recs[4].Kind != "SelectionTell" {
		t.Errorf("line 5 = %s %s; want the selection reset", recs[4].Stage, recs[4].Kind)
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

func TestWithRecorderInstallsTheRecorder(t *testing.T) {
	ring := newRingRecorder(4)
	if b := NewBackend(WithRecorder(ring)); b.rec != Recorder(ring) {
		t.Fatal("WithRecorder's recorder did not reach the backend")
	}
	if b := NewBackend(); b.rec != nil {
		t.Fatal("a backend built with no WithRecorder must record nothing")
	}
}
