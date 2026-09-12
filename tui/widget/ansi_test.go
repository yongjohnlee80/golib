package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

func TestSGRInterpColorsAndStyles(t *testing.T) {
	interp := &sgrInterp{passthrough: true}
	var events []sgrEvent
	emit := func(ev sgrEvent) {
		events = append(events, ev)
	}

	// ANSI 16 red, bold, then reset.
	interp.feed([]byte("\x1b[31;1mredbold\x1b[0mplain\n"), emit)

	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}
	if events[0].kind != sgrText || events[0].text != "redbold" {
		t.Errorf("event 0: got kind %v, text %q", events[0].kind, events[0].text)
	}
	if fg, _ := events[0].st.GetForeground(); fg != style.ANSI(1) {
		t.Errorf("event 0 FG: got %v, want ANSI(1)", fg)
	}
	if events[1].kind != sgrText || events[1].text != "plain" {
		t.Errorf("event 1: got kind %v, text %q", events[1].kind, events[1].text)
	}
	if fg, _ := events[1].st.GetForeground(); fg != style.Default() {
		t.Errorf("event 1 FG: got %v, want Default()", fg)
	}
	if events[2].kind != sgrNewline {
		t.Errorf("event 2: got kind %v, want sgrNewline", events[2].kind)
	}
}

func TestSGRInterpCarriageReturnAndTabs(t *testing.T) {
	interp := &sgrInterp{passthrough: true}
	var events []sgrEvent
	emit := func(ev sgrEvent) {
		events = append(events, ev)
	}

	// Bare \r emits sgrCarriage; \t expands to 4 spaces; \r\n emits sgrNewline.
	interp.feed([]byte("hello\rworld\r\n\tindent"), emit)

	hasCarriage := false
	hasNewline := false
	hasIndent := false
	for _, ev := range events {
		if ev.kind == sgrCarriage {
			hasCarriage = true
		}
		if ev.kind == sgrNewline {
			hasNewline = true
		}
		if ev.kind == sgrText && ev.text == "    indent" {
			hasIndent = true
		}
	}
	if !hasCarriage {
		t.Error("missing sgrCarriage from bare \\r")
	}
	if !hasNewline {
		t.Error("missing sgrNewline from \\r\\n")
	}
	if !hasIndent {
		t.Error("tab did not expand to 4 spaces")
	}
}

func TestSGRInterpPassthroughDisabled(t *testing.T) {
	interp := &sgrInterp{passthrough: false}
	var events []sgrEvent
	emit := func(ev sgrEvent) {
		events = append(events, ev)
	}

	interp.feed([]byte("\x1b[31mcolorless\x1b[0m"), emit)
	if len(events) != 1 || events[0].text != "colorless" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if fg, _ := events[0].st.GetForeground(); fg != style.Default() {
		t.Errorf("passthrough=false applied color: %v", fg)
	}
}
