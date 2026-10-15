package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/io/event"
	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/io/key"

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

// imeFrameOf queues one frame of input-method callbacks the way Gio emits them, reads it and
// tells Gio the caret, as a real frame does.
func imeFrameOf(b *Backend, r *input.Router, tag *gioState, evs ...event.Event) {
	r.Queue(evs...)
	b.readInput(r.Source(), tag, metrics{})
	b.placeCaret(r.Source(), tag, &frame{caret: image.Rect(10, 10, 11, 28), base: 14})
}

// cancelsComposition is Gio's rule for when the editor state an app reports cancels the input
// method's composition: a different selection, or snippets that disagree where they overlap.
func cancelsComposition(imeSnip key.Snippet, imeSel key.Range, app input.EditorState) bool {
	if app.Selection.Range != imeSel {
		return true
	}
	start := max(imeSnip.Start, app.Snippet.Start)
	end := min(imeSnip.End, app.Snippet.End)
	if start >= end {
		return false
	}
	over := func(s key.Snippet) string { return string([]rune(s.Text)[start-s.Start : end-s.Start]) }
	return over(imeSnip) != over(app.Snippet)
}

// macOS's Korean input method commits a syllable and begins the next one in one keystroke, but
// it runs in its own process: typing ㅇ after 군 is insertText(군), then a pause in which it asks
// for the text after the caret, then setMarkedText(ㅇ). Delivering 군 in that pause must leave
// Gio's copy of the editor where the input method has it. Emptying it back to 0 moved the
// selection, Gio called discardMarkedText, and the ㅇ never arrived: 군요 came out as 군ㅛ.
// The events are the ones recorded on macOS typing 군요.
func TestDeliveringASyllableKeepsTheNextOnesComposition(t *testing.T) {
	b, r, tag := focusedBackend(t)
	noComp := key.CompositionEvent{Start: -1, End: -1}

	imeFrameOf(b, r, tag, // 군 composing
		key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "군"},
		key.SnippetEvent(key.Range{Start: 0, End: 1}),
		key.CompositionEvent{Start: 0, End: 1},
		key.SelectionEvent(key.Range{Start: 1, End: 1}),
	)
	imeFrameOf(b, r, tag, // ㅇ typed: 군 committed, and the input method asks what follows it
		key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "군"},
		key.SnippetEvent(key.Range{Start: 0, End: 1}),
		noComp,
		key.SelectionEvent(key.Range{Start: 1, End: 1}),
		key.SnippetEvent(key.Range{Start: 1, End: 2}),
	)
	if got := delivered(b); got != "군" {
		t.Fatalf("after the commit the App has %q, want 군", got)
	}
	ime := key.Snippet{Range: key.Range{Start: 0, End: 1}, Text: "군"}
	if st := r.EditorState(); cancelsComposition(ime, key.Range{Start: 1, End: 1}, st) {
		t.Fatalf("after delivering 군 the app reports snippet %+v, selection %v; the input method has %+v, selection 1..1, so Gio cancels the ㅇ it is about to compose",
			st.Snippet, st.Selection.Range, ime)
	}

	imeFrameOf(b, r, tag, // the ㅇ arrives, marked where Gio's selection is
		key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: "ㅇ"},
		key.SnippetEvent(key.Range{Start: 1, End: 2}),
		key.CompositionEvent{Start: 1, End: 2},
		key.SelectionEvent(key.Range{Start: 2, End: 2}),
	)
	if pre := b.gio.ime.preedit(); pre != "ㅇ" {
		t.Fatalf("the composition is %q, want ㅇ", pre)
	}
	if st := r.EditorState(); st.Snippet != (key.Snippet{Range: key.Range{Start: 1, End: 2}, Text: "ㅇ"}) || st.Selection.Range != (key.Range{Start: 2, End: 2}) {
		t.Fatalf("composing ㅇ the app reports snippet %+v, selection %v; want the input method's 1..2 ㅇ, selection 2..2", st.Snippet, st.Selection.Range)
	}
	imeFrameOf(b, r, tag, // ㅛ: 요 composing
		key.EditEvent{Range: key.Range{Start: 1, End: 2}, Text: "요"},
		key.SnippetEvent(key.Range{Start: 1, End: 2}),
		key.CompositionEvent{Start: 1, End: 2},
		key.SelectionEvent(key.Range{Start: 2, End: 2}),
	)
	imeFrameOf(b, r, tag, // a space: 요 committed
		key.EditEvent{Range: key.Range{Start: 1, End: 2}, Text: "요"},
		key.SnippetEvent(key.Range{Start: 1, End: 2}),
		noComp,
		key.SelectionEvent(key.Range{Start: 2, End: 2}),
	)
	if got := delivered(b); got != "군요" {
		t.Fatalf("the App has %q, want 군요", got)
	}
}

// After Gio's coordinates start again (a focus change), an edit at 0 lands in the buffer and its
// snippet is answered where Gio has it, however far deliveries had advanced the buffer before.
func TestTheBufferFollowsTheToolkitWhenItsCoordinatesStartAgain(t *testing.T) {
	b, r, tag := focusedBackend(t)
	imeFrameOf(b, r, tag,
		key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "abc"},
		key.SelectionEvent(key.Range{Start: 3, End: 3}),
	)
	imeFrameOf(b, r, tag,
		key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "ㅎ"},
		key.SnippetEvent(key.Range{Start: 0, End: 1}),
		key.CompositionEvent{Start: 0, End: 1},
		key.SelectionEvent(key.Range{Start: 1, End: 1}),
	)
	if pre := b.gio.ime.preedit(); pre != "ㅎ" {
		t.Fatalf("the composition is %q, want ㅎ", pre)
	}
	if st := r.EditorState(); st.Snippet != (key.Snippet{Range: key.Range{Start: 0, End: 1}, Text: "ㅎ"}) || st.Selection.Range != (key.Range{Start: 1, End: 1}) {
		t.Fatalf("the app reports snippet %+v, selection %v; want 0..1 ㅎ, selection 1..1", st.Snippet, st.Selection.Range)
	}
}

// delivered is the text the App has received, with Enter as ⏎.
func delivered(b *Backend) string {
	var s string
	for _, e := range b.q.queue {
		if k, ok := e.(tui.KeyEvent); ok {
			if k.Code == tui.KeyEnter {
				s += "⏎"
			} else {
				s += k.Text
			}
		}
	}
	return s
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
