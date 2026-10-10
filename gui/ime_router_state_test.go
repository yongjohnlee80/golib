package gui

import (
	"image"
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"

	"github.com/yongjohnlee80/golib/tui"
)

// focusedBackend is a backend that has taken the focus of a real Gio router.
func focusedBackend(t *testing.T) (*Backend, *input.Router, *gioState) {
	t.Helper()
	r := new(input.Router)
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	b.readInput(r.Source(), tag, metrics{})
	b.readInput(r.Source(), tag, metrics{})
	b.q.queue = nil
	return b, r, tag
}

// Gio cancels a composition when the editor state the app reports (the router's) differs from
// the one the input method edited (shouldCancelComposition). fcitx5 composing ㅇ arrives as
// Gio's callbacks emit it: the edit, the snippet Gio asks the app for, the composition and the
// input method's selection. After a frame the router must hold the same snippet and selection;
// unanswered, every Hangul stage was erased a few milliseconds after it appeared.
func TestTheRouterKeepsTheInputMethodsEditorState(t *testing.T) {
	for _, tc := range []struct {
		name string
		sel  key.Range
	}{
		{"cursor at the composition's end (fcitx5 Hangul)", key.Range{Start: 1, End: 1}},
		{"cursor at the composition's start", key.Range{Start: 0, End: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, r, tag := focusedBackend(t)
			r.Queue(
				key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "ㅇ"},
				key.SnippetEvent(key.Range{Start: 0, End: 1}),
				key.CompositionEvent{Start: 0, End: 1},
				key.SelectionEvent(tc.sel),
			)
			b.readInput(r.Source(), tag, metrics{})
			b.placeCaret(r.Source(), tag, &frame{caret: image.Rect(10, 10, 11, 28), base: 14})
			st := r.EditorState()
			if want := (key.Snippet{Range: key.Range{Start: 0, End: 1}, Text: "ㅇ"}); st.Snippet != want {
				t.Errorf("router snippet %+v, want the input method's %+v", st.Snippet, want)
			}
			if st.Selection.Range != tc.sel {
				t.Errorf("router selection %v, want the input method's %v", st.Selection.Range, tc.sel)
			}
			if pre := b.gio.ime.preedit(); pre != "ㅇ" {
				t.Errorf("the composition is %q, want ㅇ", pre)
			}
		})
	}
}

// Committed text and the key typed after it reach the App in that order: Enter after Hangul no
// longer lands before the syllables the composition held.
func TestCommittedTextIsDeliveredBeforeTheNextKey(t *testing.T) {
	b, r, tag := focusedBackend(t)
	r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "하"})
	r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	b.readInput(r.Source(), tag, metrics{})
	var got []string
	for _, e := range b.q.queue {
		k, ok := e.(tui.KeyEvent)
		if !ok {
			continue
		}
		if k.Code == tui.KeyEnter {
			got = append(got, "Enter")
		} else {
			got = append(got, k.Text)
		}
	}
	if len(got) != 2 || got[0] != "하" || got[1] != "Enter" {
		t.Fatalf("delivered %q, want [하 Enter]", got)
	}
}
