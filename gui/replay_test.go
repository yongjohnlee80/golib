package gui

// The replay harness: a JSONL recording made by the recorder, played back into a
// fresh imeState, checking that the input path emits what the recording says it
// did. This is the payoff of the whole contract: a recording made on a macOS
// machine with a Hangul input bug becomes a unit test on any machine.
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

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// kindOf names the one Kind each Stage carries; a record outside it is not this
// build's recording.
var kindOf = map[Stage][]string{
	StageIn:   {"KeyInput", "TextEdit", "Composition", "SelectionSet", "SnippetAsk", "FocusChange"},
	StageIME:  {"imeState"},
	StageOut:  {"KeyEvent"},
	StageCmd:  {"SnippetReply", "SelectionTell", "KeyboardShow", "FocusTake"},
	StageDraw: {"preedit"},
	StageMeta: {"dropped"},
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

// replayRecording drives a fresh imeState with the recording's "in" events, in
// order, and checks the input path against the recording's own records of it:
// the state after each event ("ime"), the delivered keys ("out") and the
// commands sent back ("cmd"). The out and cmd records prove the emissions; the
// ime records prove the state that produced them — without them, a bug that
// leaves the delivery timing untouched (a composition range ignored, say) would
// replay clean. The preedit draws ("draw") and the writer's notes ("meta") are
// validated by the decode and then skipped: they follow from what is checked.
func replayRecording(t *testing.T, jsonl []byte) {
	t.Helper()
	recs, err := decodeRecording(jsonl)
	if err != nil {
		t.Fatalf("the recording does not decode: %v", err)
	}
	var produced, expected []Record
	s := new(imeState)
	deliver := func() { // what deliverIME emits when the input loop ends
		pieces, ok := s.take()
		if !ok {
			return
		}
		var out []tui.KeyEvent
		for _, p := range pieces {
			out = append(out, textKeys(p)...)
		}
		_, data := cmdFields(SnippetReply{})
		_, sel := cmdFields(SelectionTell{})
		produced = append(produced,
			Record{Stage: StageOut, Kind: "KeyEvent", Data: map[string]any{"events": out}},
			Record{Stage: StageCmd, Kind: "SnippetReply", Data: data},
			Record{Stage: StageCmd, Kind: "SelectionTell", Data: sel},
		)
	}
	for _, rec := range recs {
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
				_, data := cmdFields(s.snippet(ev))
				produced = append(produced, Record{Stage: StageCmd, Kind: "SnippetReply", Data: data})
			case FocusChange: // the window's focus, not the buffer's
			case KeyInput:
				// Not recorded as an in-event by today's window, whose key events
				// keep their own path; committed text goes before the key, as
				// readInput delivers it, and no state snapshot follows.
				deliver()
				continue
			}
			// The live path snapshots the buffer after every IME event, and the
			// replay's snapshot must be the one the recording holds.
			produced = append(produced, Record{Stage: StageIME, Kind: "imeState", Data: imeFields(s)})
		case StageOut, StageCmd, StageIME:
			expected = append(expected, rec)
		}
	}
	deliver() // the loop's end, as readInput's last act
	if len(produced) != len(expected) {
		t.Fatalf("the replay emitted %d records against the recording's %d:\nreplay:    %s\nrecording: %s",
			len(produced), len(expected), canonRecords(produced), canonRecords(expected))
	}
	for i := range produced {
		p, e := canon(produced[i]), canon(expected[i])
		if p != e {
			t.Fatalf("emission %d does not match the recording:\nreplay:    %s\nrecording: %s", i, p, e)
		}
	}
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
// cell for the script that produced it), replayed: what the input path emits on a
// fresh imeState must be what the recording says it emitted when it was made.
func TestTheRecordedHangulCompositionReplays(t *testing.T) {
	data, err := os.ReadFile("testdata/hangul-composition.jsonl")
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
