package gui

// The replay harness: a JSONL recording made by the recorder, played back into a
// fresh input path, checking that it emits what the recording says it did. This
// is the payoff of the whole contract: a recording made on a macOS machine with
// a Hangul input bug becomes a unit test on any machine.
//
// # How to turn a user's --debug recording into a test
//
// The M6 --debug docs link here. A recording is a test in three lines:
//
//	func TestTheUsersRecordingReplays(t *testing.T) {
//		data, err := os.ReadFile("testdata/<name>.jsonl")
//		if err != nil {
//			t.Fatal(err)
//		}
//		replayRecording(t, data)
//	}
//
// Copy the recording file the user sent into gui/testdata/<name>.jsonl verbatim:
// the replay reads it as the recorder wrote it. The decode side rejects a Stage
// or Kind it does not know, naming the record, so a recording from a newer build
// fails loudly rather than passing by being misunderstood.
//
// # What the replay knows
//
// Frame records ("begin"/"end") are what make a whole session replayable: the
// window delivers committed text at each frame's end, so without them the
// recording cannot say when delivery happened. A recording with no frame
// records — from a build before they were written — replays as one frame, as
// it always did, with no focus modeling; a session recorded from its first
// frame needs them. The window and the replay share the steps themselves:
// [gioState.takeFocus] is the prologue, [imeState.commitFrame] is the
// end-of-frame delivery, [imeState.selection] is the selection the caret tell
// carries — the same functions, not copies. What the replay cannot know is
// layout: a caret tell's geometry comes from the frame, so the replay checks
// its selection and takes the caret as environment.

// # When a recording can be replayed
//
// The replay needs a complete trace, and says so when it does not have one. The
// frames must pair — every begin has its end, the frame numbers march strictly
// forward — nothing input-shaped may sit outside a frame's begin and end (a
// frame's presentation tail, its caret tell and its draws, may), and a recording
// that dropped records under load is refused outright: what it holds is not what
// happened, so it cannot be checked against anything. A legacy recording, with
// no frame records at all, replays as the one frame it claims to be.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// kindOf names the Kind each Stage carries; a record outside it is not this
// build's recording.
var kindOf = map[Stage][]string{
	StageIn:    {"KeyInput", "TextEdit", "Composition", "SelectionSet", "SnippetAsk", "FocusChange"},
	StageIME:   {"imeState"},
	StageOut:   {"KeyEvent"},
	StageCmd:   {"SnippetReply", "SelectionTell", "KeyboardShow", "FocusTake"},
	StageDraw:  {"preedit"},
	StageMeta:  {"dropped"},
	StageFrame: {"begin", "end"},
}

// decodeRecording reads a JSONL recording, one record per line, and rejects a
// Stage or Kind this build does not know, naming the offending record.
func decodeRecording(jsonl []byte) ([]Record, error) {
	var out []Record
	for n, line := range strings.Split(strings.TrimRight(string(jsonl), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, err
		}
		kinds, known := kindOf[rec.Stage]
		if !known {
			return nil, errf("record %d (line %d) has stage %q, which this build does not know", rec.Seq, n+1, rec.Stage)
		}
		if !contains(kinds, rec.Kind) {
			return nil, errf("record %d (line %d) has %s-kind %q, which this build does not know", rec.Seq, n+1, rec.Stage, rec.Kind)
		}
		out = append(out, rec)
	}
	return out, nil
}

// errf builds a replay error.
func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }

// contains reports whether s is one of the list.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// decodeInputEvent turns one "in" record back into the InputEvent it recorded.
func decodeInputEvent(rec Record) (InputEvent, error) {
	num := func(field string) (int, error) {
		v, ok := rec.Data[field].(float64)
		if !ok {
			return 0, errf("record %d (%s %s): field %q is %T, want a number", rec.Seq, rec.Stage, rec.Kind, field, rec.Data[field])
		}
		return int(v), nil
	}
	switch rec.Kind {
	case "TextEdit":
		start, err := num("start")
		if err != nil {
			return nil, err
		}
		end, err := num("end")
		if err != nil {
			return nil, err
		}
		text, _ := rec.Data["text"].(string)
		return TextEdit{Start: start, End: end, Text: text}, nil
	case "Composition":
		start, err := num("start")
		if err != nil {
			return nil, err
		}
		end, err := num("end")
		if err != nil {
			return nil, err
		}
		return Composition{Start: start, End: end}, nil
	case "SelectionSet":
		start, err := num("start")
		if err != nil {
			return nil, err
		}
		end, err := num("end")
		if err != nil {
			return nil, err
		}
		return SelectionSet{Start: start, End: end}, nil
	case "SnippetAsk":
		start, err := num("start")
		if err != nil {
			return nil, err
		}
		end, err := num("end")
		if err != nil {
			return nil, err
		}
		return SnippetAsk{Start: start, End: end}, nil
	case "FocusChange":
		focused, _ := rec.Data["focused"].(bool)
		return FocusChange{Focused: focused}, nil
	case "KeyInput":
		name, _ := rec.Data["name"].(string)
		mods, ok := rec.Data["mods"].(float64)
		if !ok {
			return nil, errf("record %d (KeyInput): field \"mods\" is %T, want a number", rec.Seq, rec.Data["mods"])
		}
		press, _ := rec.Data["press"].(bool)
		return KeyInput{Name: name, Mods: Mods(uint8(mods)), Press: press}, nil
	}
	return nil, errf("record %d has in-kind %q, which this build does not know", rec.Seq, rec.Kind)
}

// splitFrames segments a recording at its "begin" records. A recording with
// none — from a build before frames were recorded — is one frame, replayed end
// to end as it always was.
func splitFrames(recs []Record) [][]Record {
	var frames [][]Record
	started := false
	var cur []Record
	for _, rec := range recs {
		if rec.Stage == StageFrame && rec.Kind == "begin" {
			if started {
				frames = append(frames, cur)
			}
			started = true
			cur = nil
			continue
		}
		cur = append(cur, rec)
	}
	return append(frames, cur)
}

// hasFrames reports whether the recording carries frame records at all.
func hasFrames(recs []Record) bool {
	for _, rec := range recs {
		if rec.Stage == StageFrame {
			return true
		}
	}
	return false
}

// validateTrace checks a recording's integrity before it is replayed: every
// frame's begin must have its end, the frame numbers must march strictly
// forward, nothing input-shaped may sit outside a frame's begin and end (a
// frame's presentation tail — its caret tell, its draws — may), and a recording
// that dropped records under load is refused outright: what it holds is not what
// happened. A legacy recording, with no frame records, is exempt from the frame
// rules and replays as the one frame it claims to be.
func validateTrace(recs []Record) error {
	framed, inside := false, false
	last := 0
	for _, rec := range recs {
		if rec.Stage == StageMeta && rec.Kind == "dropped" {
			return errf("recording dropped %v records under load; re-record (replay needs a complete trace)", rec.Data["n"])
		}
		if rec.Stage == StageDraw || (rec.Stage == StageCmd && rec.Kind == "SelectionTell") {
			continue // a frame's presentation tail, inside its pair or after its end
		}
		if rec.Stage != StageFrame {
			if framed && !inside {
				return errf("record Seq %d outside a frame", rec.Seq)
			}
			continue
		}
		n, err := frameNum(rec)
		if err != nil {
			return err
		}
		switch rec.Kind {
		case "begin":
			if inside {
				return errf("frame %d begin has no end", last)
			}
			if framed && n <= last {
				return errf("frame n went from %d to %d", last, n)
			}
			framed, inside, last = true, true, n
		case "end":
			if !inside {
				return errf("end n=%d without begin", n)
			}
			if n != last {
				return errf("frame %d's end carries n=%d", last, n)
			}
			inside = false
		}
	}
	if inside {
		return errf("frame %d begin has no end", last)
	}
	return nil
}

// frameNum reads a frame record's number.
func frameNum(rec Record) (int, error) {
	v, ok := rec.Data["n"].(float64)
	if !ok {
		return 0, errf("frame record Seq %d carries no n", rec.Seq)
	}
	return int(v), nil
}

// replayRecording plays a recording back through a fresh input path and checks
// every frame of it: the focus handshake the window's prologue runs, the state
// after each event, the commands the snippet answers send, the delivery at each
// frame's end, and the selection the frame's caret tell carries. What layout
// contributes — the caret's geometry — is checked as environment: taken from
// the recording, not reproduced.
func replayRecording(t *testing.T, jsonl []byte) {
	t.Helper()
	recs, err := decodeRecording(jsonl)
	if err != nil {
		t.Fatalf("the recording does not decode: %v", err)
	}
	if err := validateTrace(recs); err != nil {
		t.Fatalf("the recording is not a complete trace: %v", err)
	}
	framed := hasFrames(recs)
	g := new(gioState) // the window's own focus bookkeeping
	s := new(imeState)
	for fi, frame := range splitFrames(recs) {
		var produced, expected []Record
		var carets []Record
		commit := func() { // the frame's end, as readInput's last act
			keys, resets, ok := s.commitFrame()
			if !ok {
				return
			}
			produced = append(produced, Record{Stage: StageOut, Kind: "KeyEvent", Data: map[string]any{"events": keys}})
			for _, cmd := range resets {
				kind, data := cmdFields(cmd)
				produced = append(produced, Record{Stage: StageCmd, Kind: kind, Data: data})
			}
		}
		if framed { // a frame-aware recording carries the prologue the window ran
			for _, cmd := range g.takeFocus() {
				kind, data := cmdFields(cmd)
				produced = append(produced, Record{Stage: StageCmd, Kind: kind, Data: data})
			}
		}
		for _, rec := range frame {
			switch rec.Stage {
			case StageIn:
				ev, err := decodeInputEvent(rec)
				if err != nil {
					t.Fatalf("the recording does not decode: %v", err)
				}
				switch ev := ev.(type) {
				case TextEdit:
					s.edit(ev)
				case Composition:
					s.compose(ev)
				case SelectionSet:
					s.setSelection(ev)
				case SnippetAsk:
					// The answer the live path sent back is a command record, before
					// the state snapshot the live path takes after it.
					kind, data := cmdFields(s.snippet(ev))
					produced = append(produced, Record{Stage: StageCmd, Kind: kind, Data: data})
				case FocusChange:
					g.focused = ev.Focused // the window's focus, not the buffer's
				case KeyInput:
					// Not recorded as an in-event by today's window, whose key events
					// keep their own path; committed text goes before the key, as
					// readInput delivers it, and no state snapshot follows.
					commit()
					continue
				}
				produced = append(produced, Record{Stage: StageIME, Kind: "imeState", Data: imeFields(s)})
			case StageIME, StageOut:
				expected = append(expected, rec)
			case StageCmd:
				if rec.Kind == "SelectionTell" && caretTold(rec) {
					// A frame's caret tell: layout's contribution, checked below.
					carets = append(carets, rec)
					continue
				}
				expected = append(expected, rec)
			}
		}
		commit() // the loop's end, as readInput's last act
		if len(produced) != len(expected) {
			t.Fatalf("frame %d: the replay emitted %d records against the recording's %d:\nreplay:    %s\nrecording: %s",
				fi+1, len(produced), len(expected), canonRecords(produced), canonRecords(expected))
		}
		for i := range produced {
			p, e := canon(produced[i]), canon(expected[i])
			if p != e {
				t.Fatalf("frame %d, emission %d does not match the recording:\nreplay:    %s\nrecording: %s", fi+1, i, p, e)
			}
		}
		// The frame's caret tell, if it had one: the selection the input method
		// should see, with the caret taken as the environment it is.
		for _, caret := range carets {
			start, errStart := caretField(caret, "start")
			end, errEnd := caretField(caret, "end")
			if errStart != nil || errEnd != nil {
				t.Fatalf("frame %d: the caret tell does not decode: %v, %v", fi+1, errStart, errEnd)
			}
			if sel := s.selection(); sel.Start != start || sel.End != end {
				t.Fatalf("frame %d: the caret tell says selection %d..%d, the replay's is %d..%d", fi+1, start, end, sel.Start, sel.End)
			}
		}
	}
}

// caretTold reports whether a SelectionTell record carries a caret place: the
// resets deliverIME sends are empty, and a frame's caret tell is not.
func caretTold(rec Record) bool {
	caret, ok := rec.Data["caret"].(map[string]any)
	if !ok {
		return false
	}
	for _, field := range []string{"x", "y", "ascent", "descent"} {
		if v, ok := caret[field].(float64); ok && v != 0 {
			return true
		}
	}
	return false
}

// caretField reads one numeric field of a record.
func caretField(rec Record, field string) (int, error) {
	v, ok := rec.Data[field].(float64)
	if !ok {
		return 0, errf("record %d (%s %s): field %q is %T, want a number", rec.Seq, rec.Stage, rec.Kind, field, rec.Data[field])
	}
	return int(v), nil
}

// canon renders a record's stage, kind and data as JSON both sides of a replay
// agree on. The values are round-tripped through decoding first: the recording's
// data comes back as maps (sorted keys) while a replayed emission holds structs
// (declared fields), and the round trip puts both in the same map domain.
func canon(rec Record) string {
	b, err := json.Marshal(Record{Stage: rec.Stage, Kind: rec.Kind, Data: rec.Data})
	if err != nil {
		return "«unmarshallable»"
	}
	var norm any
	if err := json.Unmarshal(b, &norm); err != nil {
		return "«undecodable»"
	}
	out, err := json.Marshal(norm)
	if err != nil {
		return "«unmarshallable»"
	}
	return string(out)
}

// canonRecords renders a record list, one per line, for failure messages.
func canonRecords(recs []Record) string {
	var lines []string
	for _, rec := range recs {
		lines = append(lines, canon(rec))
	}
	return strings.Join(lines, "\n\t   ")
}

// The checked-in recording of the scripted Hangul composition (see recorder_test's
// cell for the script that produced it), from the build before frames were
// recorded: it replays as the one frame it is.
func TestTheRecordedHangulCompositionReplays(t *testing.T) {
	data, err := os.ReadFile("testdata/hangul-composition.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	replayRecording(t, data)
}

// A real session, recorded frame by frame: the window takes focus, plain typing
// commits at its frame's end, a Hangul composition is held across a frame
// boundary, and the caret tell follows each frame. Replayed clean.
func TestTheRecordedMultiFrameSessionReplays(t *testing.T) {
	data, err := os.ReadFile("testdata/hangul-multiframe.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	replayRecording(t, data)
}

// A recording from a newer build — a Stage or Kind this one does not know — must
// fail loudly, not pass by being misunderstood.
func TestTheReplayRejectsWhatItDoesNotKnow(t *testing.T) {
	newer := `{"Seq":0,"At":"2026-01-01T00:00:00Z","Stage":"in","Kind":"TextEdit","Data":{"start":0,"end":0,"text":"a"}}
{"Seq":1,"At":"2026-01-01T00:00:00Z","Stage":"tui","Kind":"Trace","Data":{}}`
	if _, err := decodeRecording([]byte(newer)); err == nil || !strings.Contains(err.Error(), `"tui"`) {
		t.Fatalf("a newer build's stage passed the decode: %v", err)
	}
	unknownKind := `{"Seq":0,"At":"2026-01-01T00:00:00Z","Stage":"in","Kind":"Gesture","Data":{}}`
	if _, err := decodeRecording([]byte(unknownKind)); err == nil || !strings.Contains(err.Error(), `"Gesture"`) {
		t.Fatalf("an unknown in-kind passed the decode: %v", err)
	}
}

// An incomplete trace must be refused, each break named: a begin without its
// end, an end without its begin, input outside any frame, a frame number that
// does not march forward, and records the writer dropped under load — what such
// a recording holds is not what happened, so it cannot be replayed green.
func TestABrokenTraceIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
		want string
	}{
		{"missing end", "testdata/broken-missing-end.jsonl", "frame 1 begin has no end"},
		{"end without begin", "testdata/broken-unmatched-end.jsonl", "end n=2 without begin"},
		{"input outside a frame", "testdata/broken-outside.jsonl", "record Seq 3 outside a frame"},
		{"frame number not forward", "testdata/broken-repeat-n.jsonl", "frame n went from 1 to 1"},
		{"dropped records", "testdata/broken-dropped.jsonl", "recording dropped 3 records under load; re-record (replay needs a complete trace)"},
	} {
		data, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		recs, err := decodeRecording(data)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		err = validateTrace(recs)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: validateTrace says %q; want it to say %q", tc.name, err, tc.want)
		}
	}
}
