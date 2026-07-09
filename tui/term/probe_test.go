package term

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// noEnv is the hermetic environment stub: probe tests must not inherit the
// developer's COLORTERM/TERM.
func noEnv(string) (string, bool) { return "", false }

// termScript is the scripted pty-less harness of ADR-0002 §5.3: the backend
// reads canned replies from an in-memory pipe and its writes are captured.
type termScript struct {
	t  *testing.T
	pr *io.PipeReader
	pw *io.PipeWriter
	w  *countingWriter
	b  *Backend
}

func newScript(t *testing.T, opts ...Option) *termScript {
	t.Helper()
	pr, pw := io.Pipe()
	w := &countingWriter{}
	opts = append([]Option{WithEnv(noEnv)}, opts...)
	b := newHarness(pr, w, opts...)
	s := &termScript{t: t, pr: pr, pw: pw, w: w, b: b}
	t.Cleanup(func() {
		_ = s.b.Stop()
		_ = s.pw.Close()
	})
	return s
}

// respond waits for the probe batch (its DA1 fence tail) to appear on the
// output, then feeds the canned reply bytes.
func (s *termScript) respond(replies string) {
	go func() {
		if !s.waitOutput("\x1b[c", 5*time.Second) {
			return // Start already gave up; nothing to answer
		}
		_, _ = s.pw.Write([]byte(replies))
	}()
}

func (s *termScript) waitOutput(substr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.w.String(), substr) {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func (s *termScript) start(ctx context.Context) error {
	s.t.Helper()
	return s.b.Start(ctx)
}

// write feeds post-start user input.
func (s *termScript) write(input string) {
	s.t.Helper()
	if _, err := s.pw.Write([]byte(input)); err != nil {
		s.t.Fatalf("script write: %v", err)
	}
}

func (s *termScript) waitEvent(timeout time.Duration) (tui.Event, bool) {
	select {
	case ev, ok := <-s.b.Events():
		return ev, ok
	case <-time.After(timeout):
		return nil, false
	}
}

// fullModernReplies answers every probe row (ADR-0002 §2.6 table) in request
// order: DECRPM 2004/2026/2027/2048/1006 all set, XTGETTCAP RGB+Smulx,
// OSC 10/11, kitty flags, then the DA1 fence.
const fullModernReplies = "\x1b[?2004;1$y" +
	"\x1b[?2026;2$y" +
	"\x1b[?2027;1$y" +
	"\x1b[?2048;2$y" +
	"\x1b[?1006;1$y" +
	"\x1bP1+r524742=1;536D756C78=1\x1b\\" +
	"\x1b]10;rgb:ffff/ffff/ffff\x1b\\" +
	"\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\" +
	"\x1b[?0u" +
	"\x1b[?62;4;22c"

func TestProbeFullModern(t *testing.T) {
	// ADR-0002 §5.3(a): full-modern replies set every flag, including
	// Mouse == TriYes from the DECRQM ?1006 answer.
	s := newScript(t)
	s.respond(fullModernReplies)
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	caps := s.b.Capabilities()
	want := tui.Capabilities{
		ColorProfile:   tui.ProfileTrueColor,
		KittyKeyboard:  true,
		SyncOutput:     true,
		InBandResize:   true,
		UnicodeCore:    true,
		BracketedPaste: true,
		Mouse:          tui.TriYes,
		Undercurl:      true,
		DarkBackground: true,
		DefaultFG:      tui.ProbedColor{R: 255, G: 255, B: 255, Known: true},
		DefaultBG:      tui.ProbedColor{R: 0x1e, G: 0x1e, B: 0x1e, Known: true},
	}
	if caps != want {
		t.Fatalf("caps mismatch\n got: %+v\nwant: %+v", caps, want)
	}

	// ADR-0002 §5.4: kitty push emitted iff the probe reported support,
	// plus the negotiated mode enables (§2.6), all after the fence.
	out := s.w.String()
	for _, seq := range []string{"\x1b[?2004h", "\x1b[?1002h\x1b[?1006h", "\x1b[>3u", "\x1b[?2048h", "\x1b[?1004h"} {
		if !strings.Contains(out, seq) {
			t.Errorf("enable write missing %q", seq)
		}
	}
}

func TestProbeDA1Only(t *testing.T) {
	// ADR-0002 §5.3(b): DA1-only replies return before the deadline with
	// Mouse == TriUnknown, the mode flags false, and DarkBackground == true
	// (the documented assume-dark fallback).
	s := newScript(t, WithProbeTimeout(time.Second))
	s.respond("\x1b[?62c")
	startAt := time.Now()
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startAt); elapsed > 500*time.Millisecond {
		t.Fatalf("DA1 fence did not short-circuit the deadline: %v", elapsed)
	}
	caps := s.b.Capabilities()
	want := tui.Capabilities{
		ColorProfile:   tui.ProfileANSI16,
		Mouse:          tui.TriUnknown,
		DarkBackground: true,
	}
	if caps != want {
		t.Fatalf("caps mismatch\n got: %+v\nwant: %+v", caps, want)
	}
	if strings.Contains(s.w.String(), "\x1b[>3u") {
		t.Error("kitty push emitted without probe support (§5.4)")
	}
	// The unverified mouse enable is still attempted (TriUnknown != TriNo),
	// but Capabilities keeps reporting TriUnknown — capability honesty.
	if !strings.Contains(s.w.String(), "\x1b[?1006h") {
		t.Error("mouse enable not attempted on TriUnknown")
	}
}

func TestProbeSilenceHitsDeadline(t *testing.T) {
	// ADR-0002 §5.3(c): total silence returns at the deadline with the same
	// defaults. 60ms probe timeout (the [50ms, 1s] clamp keeps it).
	s := newScript(t, WithProbeTimeout(60*time.Millisecond))
	startAt := time.Now()
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(startAt)
	if elapsed < 50*time.Millisecond {
		t.Fatalf("returned before the deadline: %v", elapsed)
	}
	caps := s.b.Capabilities()
	want := tui.Capabilities{
		ColorProfile:   tui.ProfileANSI16,
		Mouse:          tui.TriUnknown,
		DarkBackground: true,
	}
	if caps != want {
		t.Fatalf("caps mismatch\n got: %+v\nwant: %+v", caps, want)
	}
}

func TestProbePreseedSurvivesSilence(t *testing.T) {
	// ADR-0002 §5.3(d): $COLORTERM=truecolor pre-seed survives probe
	// silence as ProfileTrueColor — never downgraded by silence.
	env := func(k string) (string, bool) {
		if k == "COLORTERM" {
			return "truecolor", true
		}
		return "", false
	}
	s := newScript(t, WithEnv(env), WithProbeTimeout(60*time.Millisecond))
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := s.b.Capabilities().ColorProfile; got != tui.ProfileTrueColor {
		t.Fatalf("pre-seed lost: %v", got)
	}
}

func TestPreseedProfile(t *testing.T) {
	// ADR-0002 §2.6 pre-seed derivation (no I/O).
	envOf := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	cases := []struct {
		name string
		env  map[string]string
		want tui.ColorProfile
	}{
		{"empty", nil, tui.ProfileANSI16},
		{"256color", map[string]string{"TERM": "xterm-256color"}, tui.ProfileANSI256},
		{"colorterm truecolor", map[string]string{"COLORTERM": "truecolor"}, tui.ProfileTrueColor},
		{"colorterm 24bit", map[string]string{"COLORTERM": "24bit"}, tui.ProfileTrueColor},
		{"colorterm junk", map[string]string{"COLORTERM": "yes"}, tui.ProfileANSI16},
		{"wt session", map[string]string{"WT_SESSION": "guid"}, tui.ProfileTrueColor},
		{"term program", map[string]string{"TERM_PROGRAM": "WezTerm"}, tui.ProfileTrueColor},
		{"256 + truecolor wins", map[string]string{"TERM": "screen-256color", "COLORTERM": "truecolor"}, tui.ProfileTrueColor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := preseedProfile(envOf(tc.env)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestProbeLightBackground(t *testing.T) {
	// OSC 11 reporting a light background flips DarkBackground off.
	s := newScript(t)
	s.respond("\x1b]11;rgb:ffff/ffff/ffff\x1b\\" + "\x1b[?62c")
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	caps := s.b.Capabilities()
	if caps.DarkBackground {
		t.Fatal("white background reported dark")
	}
	if !caps.DefaultBG.Known || caps.DefaultBG.R != 255 {
		t.Fatalf("DefaultBG wrong: %+v", caps.DefaultBG)
	}
}

func TestProbeRepliesAfterFenceDiscarded(t *testing.T) {
	// ADR-0002 §5.3(e): replies after the fence are discarded harmlessly —
	// no events, no capability mutation.
	s := newScript(t)
	s.respond("\x1b[?62c")
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := s.b.Capabilities()
	s.write("\x1b[?2026;1$y\x1b[?0u\x1b]11;rgb:0000/0000/0000\x07")
	if ev, ok := s.waitEvent(100 * time.Millisecond); ok {
		t.Fatalf("late probe reply leaked as event: %#v", ev)
	}
	if got := s.b.Capabilities(); got != before {
		t.Fatalf("late replies mutated capabilities: %+v -> %+v", before, got)
	}
}

func TestProbeCtxCancel(t *testing.T) {
	// ADR-0002 §5.3(f): ctx cancellation mid-probe discards partial
	// replies, restores acquired terminal state, and returns ctx.Err().
	s := newScript(t, WithProbeTimeout(time.Second))
	// Answer two rows but never the fence: the probe stays in flight and
	// the partial replies must not become observable.
	s.respond("\x1b[?2004;1$y\x1b[?2026;1$y")
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		// Let the batch go out and the partial replies land, then cancel
		// while the probe is still waiting on the fence.
		s.waitOutput("\x1b[c", 5*time.Second)
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	err := s.start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start = %v, want context.Canceled", err)
	}
	// A partially-negotiated profile is never observable.
	if got := s.b.Capabilities(); got != (tui.Capabilities{}) {
		t.Fatalf("partial capabilities leaked: %+v", got)
	}
	// Acquired state was unwound: the alternate screen is left again.
	if !strings.Contains(s.w.String(), "\x1b[?1049l") {
		t.Fatal("alt screen not restored on ctx cancel")
	}
	// The failed Start already ran Stop: Events() is closed, Err() is nil.
	select {
	case _, ok := <-s.b.Events():
		if ok {
			t.Fatal("unexpected event after cancelled Start")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events() not closed after cancelled Start")
	}
	if err := s.b.Err(); err != nil {
		t.Fatalf("Err() after cancelled Start = %v, want nil", err)
	}
}
