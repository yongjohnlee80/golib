package gui

import (
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/io/event"
	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/io/key"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/golib/tui"
)

func TestInputEventOf(t *testing.T) {
	press := func(n key.Name, m key.Modifiers, s key.State) key.Event {
		return key.Event{Name: n, Modifiers: m, State: s}
	}
	cases := []struct {
		name string
		ev   event.Event
		want InputEvent
		ok   bool
	}{
		{"enter press", press(key.NameReturn, 0, key.Press), KeyInput{Name: string(key.NameReturn), Press: true}, true},
		{"ctrl-shift-a release", press("A", key.ModCtrl|key.ModShift, key.Release), KeyInput{Name: "A", Mods: tui.ModCtrl | tui.ModShift, Press: false}, true},
		// macOS's Command reaches the contract as Super, as it reaches the widgets.
		{"command-s", press("S", key.ModCommand, key.Press), KeyInput{Name: "S", Mods: tui.ModSuper, Press: true}, true},
		{"lone shift", press(key.NameShift, key.ModShift, key.Press), KeyInput{Name: string(key.NameShift), Mods: tui.ModShift, Press: true}, true},
		// Committed Hangul arrives as an edit of the range the selection left.
		{"hangul commit", key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "한"}, TextEdit{Start: 0, End: 0, Text: "한"}, true},
		{"replace range", key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "하"}, TextEdit{Start: 0, End: 1, Text: "하"}, true},
		{"composition", key.CompositionEvent{Start: 0, End: 2}, Composition{Start: 0, End: 2}, true},
		// The input method ends a composition with a negative range: the "none"
		// encoding passes through untouched.
		{"composition end", key.CompositionEvent{Start: -1, End: -1}, Composition{Start: -1, End: -1}, true},
		{"selection set", key.SelectionEvent{Start: 3, End: 1}, SelectionSet{Start: 3, End: 1}, true},
		{"snippet ask", key.SnippetEvent{Start: 0, End: 64}, SnippetAsk{Start: 0, End: 64}, true},
		{"focus gained", key.FocusEvent{Focus: true}, FocusChange{Focused: true}, true},
		{"focus lost", key.FocusEvent{Focus: false}, FocusChange{Focused: false}, true},
		// Pointer is not the contract's in v1.
		{"pointer", pointer.Event{Kind: pointer.Press}, nil, false},
	}
	for _, c := range cases {
		got, ok := inputEventOf(c.ev)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: inputEventOf = %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// recordingSource is an eventSource that records the commands executed on it.
type recordingSource struct{ cmds []input.Command }

func (r *recordingSource) Event(...event.Filter) (event.Event, bool) { return nil, false }
func (r *recordingSource) Execute(c input.Command)                   { r.cmds = append(r.cmds, c) }

func TestExecTextInputCmd(t *testing.T) {
	tag := new(bool)
	cases := []struct {
		name string
		cmd  TextInputCmd
		want input.Command
	}{
		{"snippet reply", SnippetReply{Start: 2, End: 5, Text: "한"},
			key.SnippetCmd{Tag: tag, Snippet: key.Snippet{Range: key.Range{Start: 2, End: 5}, Text: "한"}}},
		// The empty reply is the command deliverIME resets Gio's copy with.
		{"snippet reset", SnippetReply{}, key.SnippetCmd{Tag: tag}},
		{"selection tell", SelectionTell{Start: 1, End: 3, Caret: CaretPlace{X: 10, Y: 40, Ascent: 12, Descent: 4}},
			key.SelectionCmd{Tag: tag, Range: key.Range{Start: 1, End: 3}, Caret: key.Caret{Pos: f32.Pt(10, 40), Ascent: 12, Descent: 4}}},
		// And the empty tell the selection reset with no caret.
		{"selection reset", SelectionTell{}, key.SelectionCmd{Tag: tag}},
		{"keyboard show", KeyboardShow{}, key.SoftKeyboardCmd{Show: true}},
		{"focus take", FocusTake{}, key.FocusCmd{Tag: tag}},
	}
	for _, c := range cases {
		var src recordingSource
		execTextInputCmd(&src, tag, c.cmd)
		if len(src.cmds) != 1 || src.cmds[0] != c.want {
			t.Errorf("%s: executed %+v; want %+v", c.name, src.cmds, c.want)
		}
	}
}
