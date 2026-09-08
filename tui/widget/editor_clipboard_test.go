package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A raw-mode TUI blocks the terminal's own selection, so a vim-style editor
// owes its user a way to copy out — bufferview already does this on `y`. These
// cells pin WHICH keys export, because the register seam they share with
// deletes would have exported text nobody asked to share.

// visual `y` DELIVERS THE EXACT BYTES, multi-byte selection included.
func TestEditorYank_VisualYankDeliversExactUTF8(t *testing.T) {
	h, ed, _ := focusedEditor(t, 40, 6)
	rec := record[widget.YankEvent](h)

	// Multi-byte on purpose: a byte-indexed range would truncate or split a
	// rune here, and the clipboard would carry mojibake that still "looks
	// copied".
	insertLines(h, "日本語テキスト")
	h.inject(key('0'), key('v'), key('$'), key('y'))
	h.settle()

	got := string(h.tb.Clipboard())
	if got != "日本語テキスト" {
		t.Errorf("clipboard = %q, want %q", got, "日本語テキスト")
	}

	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("expected exactly one YankEvent, got %d: %+v", len(evs), evs)
	}
	if !evs[0].ClipboardDelivered {
		t.Error("the event reports the copy as undelivered although the backend took it")
	}
	var owner tui.NodeID
	h.onLoop(func() { owner = ed.NodeID() })
	if evs[0].Owner != owner {
		t.Errorf("YankEvent.Owner = %v, want the editor's node %v", evs[0].Owner, owner)
	}
}

// `yy` and `2yy` DELIVER, and the second is what a count-prefixed yank looks
// like — the same call site, so the count must not skip the export.
func TestEditorYank_LinewiseYankDelivers(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []tui.Event
		want string
	}{
		{"yy", []tui.Event{key('y'), key('y')}, "alpha"},
		{"2yy", []tui.Event{key('2'), key('y'), key('y')}, "alpha\nbeta"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := focusedEditor(t, 40, 6)
			rec := record[widget.YankEvent](h)
			insertLines(h, "alpha", "beta", "gamma")
			h.inject(key('g'), key('g'))
			h.inject(tc.keys...)
			h.settle()

			if got := string(h.tb.Clipboard()); got != tc.want {
				t.Errorf("clipboard = %q, want %q", got, tc.want)
			}
			if rec.count() != 1 {
				t.Errorf("expected one YankEvent, got %d", rec.count())
			}
		})
	}
}

// DELETES DO NOT REACH THE SYSTEM CLIPBOARD.
//
// This is the cell that decides the seam, and the reason the export does not
// live in yankSet: `x`, `D`, `dd` and visual `d` all fill the same register --
// correctly, that is vim -- so exporting there would push DELETED text out
// over OSC 52. Delete a line holding a secret and it lands in the clipboard of
// whoever ran the app. It must fail if the export ever migrates back into
// yankSet.
func TestEditorYank_DeletesNeverTouchTheSystemClipboard(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []tui.Event
	}{
		{"x", []tui.Event{key('x')}},
		{"D", []tui.Event{keyShift('d')}},
		{"dd", []tui.Event{key('d'), key('d')}},
		{"2dd", []tui.Event{key('2'), key('d'), key('d')}},
		{"visual d", []tui.Event{key('v'), key('$'), key('d')}},
		{"visual x", []tui.Event{key('v'), key('$'), key('x')}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, ed, _ := focusedEditor(t, 40, 6)
			rec := record[widget.YankEvent](h)
			insertLines(h, "secret-value", "second")
			h.inject(key('g'), key('g'), key('0'))
			h.inject(tc.keys...)
			h.settle()

			if got := h.tb.Clipboard(); len(got) != 0 {
				t.Errorf("%s exported %q to the system clipboard", tc.name, string(got))
			}
			if rec.count() != 0 {
				t.Errorf("%s published a YankEvent (%d), so a parent would report a copy "+
					"that nobody asked for", tc.name, rec.count())
			}
			// AND THE REGISTER IS STILL FILLED — this must remove the export,
			// not the yank. `p` puts it back.
			h.inject(key('p'))
			h.settle()
			val, _, _, _ := edState(h, ed)
			if val == "" {
				t.Errorf("%s left the register empty; the internal yank was broken, not just "+
					"the export", tc.name)
			}
		})
	}
}

// noClipboard hides the optional ClipboardWriter capability. Embedding the
// INTERFACE rather than the concrete backend is what does it: WriteClipboard
// is not part of tui.Backend, so it is not promoted.
type noClipboard struct{ tui.Backend }

// A BACKEND WITHOUT CLIPBOARD SUPPORT IS NOT AN ERROR.
//
// The capability is optional by design — CopyToClipboard reports false rather
// than failing — so a yank must still fill the register and the event must say
// false TRUTHFULLY, which is what lets a parent tell the user their copy did
// not leave the application instead of implying it did.
func TestEditorYank_UnsupportedBackendReportsUndelivered(t *testing.T) {
	ed := widget.NewEditor()
	sh := newShell(ed)
	// The wrapper hides ClipboardWriter, and the App must therefore report
	// false from CopyToClipboard rather than failing.
	//
	// Keys go into THIS backend, not through h.inject: the harness owns a
	// TestBackend of its own, so injecting there would feed a backend the App
	// is not reading -- which is why the first version of this cell timed out
	// on the input barrier rather than testing anything.
	tb := tui.NewTestBackend(40, 6)
	h := startAppOpts(t, sh, 40, 6, tui.WithBackend(noClipboard{tb}))
	inject := func(evs ...tui.Event) {
		for _, ev := range evs {
			if err := tb.Inject(ev); err != nil {
				t.Fatal(err)
			}
		}
		h.settle()
	}
	inject(tab())

	rec := record[widget.YankEvent](h)
	inject(key('i'))
	inject(typeString("plain")...)
	inject(key(tui.KeyEscape))
	inject(key('y'), key('y'))

	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("expected one YankEvent even without clipboard support, got %d", len(evs))
	}
	if evs[0].ClipboardDelivered {
		t.Error("the event claims delivery on a backend that cannot copy")
	}
	// The register still works: `p` duplicates the line. This must remove the
	// EXPORT on such a backend, not the yank.
	inject(key('p'))
	val, _, _, _ := edState(h, ed)
	if val != "plain\nplain" {
		t.Errorf("the internal yank stopped working without clipboard support: %q", val)
	}
}

// THE EVENT CARRIES NO TEXT.
//
// A yank's payload may be a secret — this widget is used read-only to display
// credentials — and an event is exactly what ends up in a log line or a debug
// dump. A parent already knows what it rendered; what it cannot otherwise
// learn is whether the copy landed. Asserted structurally, so adding a text
// field later fails here rather than in an incident.
func TestEditorYank_EventCarriesNoSecretText(t *testing.T) {
	h, _, _ := focusedEditor(t, 40, 6)
	rec := record[widget.YankEvent](h)
	insertLines(h, "adb_pat_supersecret.value")
	h.inject(key('y'), key('y'))
	h.settle()

	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("expected one YankEvent, got %d", len(evs))
	}
	if fmtEvent := fmtAny(evs[0]); containsSub(fmtEvent, "supersecret") {
		t.Errorf("the YankEvent carries the yanked text (%s) -- an event this shape reaches "+
			"logs and debug dumps", fmtEvent)
	}
}

// insertLines types lines into a fresh editor and returns to normal mode.
// `\n` inside typeString is just a keypress the editor does not treat as a
// break -- it produced a single-line buffer, which quietly weakened every
// linewise assertion here.
func insertLines(h *harness, lines ...string) {
	h.inject(key('i'))
	for i, l := range lines {
		if i > 0 {
			h.inject(key(tui.KeyEnter))
		}
		h.inject(typeString(l)...)
	}
	h.inject(key(tui.KeyEscape))
	h.settle()
}

func fmtAny(v any) string { return fmt.Sprintf("%#v", v) }

func containsSub(s, sub string) bool { return strings.Contains(s, sub) }
