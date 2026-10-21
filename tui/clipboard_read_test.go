package tui

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// clipAnswers records each done a read runs, on the loop.
type clipAnswers struct {
	mu   sync.Mutex
	got  []clipAnswer
	note chan struct{}
}

type clipAnswer struct {
	read ClipboardRead
	text string
	err  error
}

func newClipAnswers() *clipAnswers { return &clipAnswers{note: make(chan struct{}, 16)} }

func (c *clipAnswers) done(read ClipboardRead, text string, err error) {
	c.mu.Lock()
	c.got = append(c.got, clipAnswer{read, text, err})
	c.mu.Unlock()
	c.note <- struct{}{}
}

func (c *clipAnswers) all() []clipAnswer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]clipAnswer(nil), c.got...)
}

// await waits for n answers in all.
func (c *clipAnswers) await(t *testing.T, n int) []clipAnswer {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for len(c.all()) < n {
		select {
		case <-c.note:
		case <-deadline:
			t.Fatalf("%d clipboard answers, want %d", len(c.all()), n)
		}
	}
	return c.all()
}

// read asks for the clipboard on the loop and returns what ReadClipboard returned.
func (h *harness) read(timeout time.Duration, done func(ClipboardRead, string, error)) (r ClipboardRead, err error) {
	h.onLoop(func() { r, err = h.app.ReadClipboard(timeout, done) })
	return r, err
}

// A read is answered once, on the loop, never inside ReadClipboard: the text with its line breaks
// a paste's, or the backend's error. Two reads have their own IDs and are answered in order.
func TestReadClipboardAnswersOnceOnTheLoop(t *testing.T) {
	h := startApp(t, NewFlex(Vertical), 10, 2)
	ans := newClipAnswers()
	r1, err := h.read(time.Minute, ans.done)
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := h.read(time.Minute, ans.done)
	if r1.ID == r2.ID || h.tb.ClipboardReads() != 2 {
		t.Fatalf("reads %v %v, held %d", r1, r2, h.tb.ClipboardReads())
	}
	loop := h.loopGid()
	var answeredOn int64
	h.tb.AnswerClipboard("a\r\nb\rc", nil)
	boom := errors.New("boom")
	h.tb.AnswerClipboard("ignored", boom)
	got := ans.await(t, 2)
	h.onLoop(func() { answeredOn = goid() })
	if got[0] != (clipAnswer{r1, "a\nb\nc", nil}) || got[1].read != r2 || got[1].text != "" || !errors.Is(got[1].err, boom) {
		t.Fatalf("answers %+v", got)
	}
	if answeredOn != loop {
		t.Fatal("not on the loop")
	}
	h.sync()
	if n := len(ans.all()); n != 2 {
		t.Fatalf("answered %d times, want 2", n)
	}
}

// syncReader answers every read inside ReadClipboard, as a cached backend could.
type syncReader struct {
	*TestBackend
	text string
}

func (s *syncReader) ReadClipboard(answer func(string, error)) (func(), error) {
	answer(s.text, nil)
	return func() {}, nil
}

func TestReadClipboardNeverAnswersInsideTheCall(t *testing.T) {
	tb := NewTestBackend(10, 2)
	h := runApp(t, NewApp(NewFlex(Vertical), WithBackend(&syncReader{TestBackend: tb, text: "now"}), WithMinFrameInterval(0)), tb)
	ans := newClipAnswers()
	var during int
	h.onLoop(func() {
		if _, err := h.app.ReadClipboard(time.Minute, ans.done); err != nil {
			t.Error(err)
		}
		during = len(ans.all())
	})
	if during != 0 {
		t.Fatal("done ran inside ReadClipboard")
	}
	if got := ans.await(t, 1); got[0].text != "now" {
		t.Fatalf("answers %+v", got)
	}
}

// A backend that can't read refuses at once, and done never runs; so does one with no reader.
func TestReadClipboardUnsupported(t *testing.T) {
	tb := NewTestBackend(10, 2, WithTestClipboardUnsupported())
	h := runApp(t, NewApp(NewFlex(Vertical), WithBackend(tb), WithMinFrameInterval(0)), tb)
	ans := newClipAnswers()
	if _, err := h.read(time.Minute, ans.done); !errors.Is(err, ErrClipboardUnsupported) {
		t.Fatalf("err %v, want ErrClipboardUnsupported", err)
	}
	plain := NewTestBackend(10, 2)
	hp := runApp(t, NewApp(NewFlex(Vertical), WithBackend(struct{ Backend }{plain}), WithMinFrameInterval(0)), plain)
	if _, err := hp.read(time.Minute, ans.done); !errors.Is(err, ErrClipboardUnsupported) {
		t.Fatalf("no reader: err %v, want ErrClipboardUnsupported", err)
	}
	h.sync()
	hp.sync()
	if n := len(ans.all()); n != 0 {
		t.Fatalf("a refused read ran done %d times", n)
	}
}

// lateReader holds each answer for the test to give whenever it likes: cancel is recorded, and
// ignored, as a platform's reply already on its way would be.
type lateReader struct {
	*TestBackend
	mu        sync.Mutex
	answers   []func(string, error)
	cancelled int
}

func (l *lateReader) ReadClipboard(answer func(string, error)) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.answers = append(l.answers, answer)
	return func() { l.mu.Lock(); l.cancelled++; l.mu.Unlock() }, nil
}

// A read the clipboard doesn't answer in time is answered ErrClipboardTimeout, once, and withdrawn
// from the backend; an answer after that is dropped.
func TestReadClipboardTimesOut(t *testing.T) {
	tb := NewTestBackend(10, 2)
	lr := &lateReader{TestBackend: tb}
	h := runApp(t, NewApp(NewFlex(Vertical), WithBackend(lr), WithMinFrameInterval(0)), tb)
	ans := newClipAnswers()
	r, _ := h.read(20*time.Millisecond, ans.done)
	got := ans.await(t, 1)
	if got[0].read != r || !errors.Is(got[0].err, ErrClipboardTimeout) || got[0].text != "" {
		t.Fatalf("answers %+v", got)
	}
	lr.mu.Lock()
	cancelled, late := lr.cancelled, lr.answers[0]
	lr.mu.Unlock()
	if cancelled != 1 {
		t.Fatalf("the timeout cancelled the backend's read %d times, want 1", cancelled)
	}
	late("too late", nil)
	h.sync()
	if n := len(ans.all()); n != 1 {
		t.Fatalf("the late answer ran done again: %+v", ans.all())
	}
}

// The default timeout is used for 0, and a held read the test answers in time is not timed out.
func TestReadClipboardAnsweredBeforeItsTimeout(t *testing.T) {
	h := startApp(t, NewFlex(Vertical), 10, 2)
	ans := newClipAnswers()
	h.read(0, ans.done)
	h.tb.AnswerClipboard("in time", nil)
	if got := ans.await(t, 1); got[0].text != "in time" || got[0].err != nil {
		t.Fatalf("answers %+v", got)
	}
	var deadlines int
	h.onLoop(func() { deadlines = len(h.app.timing.timers) })
	if deadlines != 0 {
		t.Fatalf("%d deadlines left after the answer, want 0", deadlines)
	}
}

// The App stopping answers every read still waiting ErrClipboardClosed, withdrawn from the backend,
// and a read asked once it is stopping is refused at once.
func TestReadClipboardClosedByStop(t *testing.T) {
	ans := newClipAnswers()
	var refused error
	var app *App
	tb := NewTestBackend(10, 2)
	app = NewApp(NewFlex(Vertical), WithBackend(tb), WithMinFrameInterval(0), WithOnStopping(func() {
		_, refused = app.ReadClipboard(time.Minute, ans.done)
	}))
	h := runApp(t, app, tb)
	r, _ := h.read(time.Minute, ans.done)
	h.wait()
	got := ans.all()
	if len(got) != 1 || got[0].read != r || !errors.Is(got[0].err, ErrClipboardClosed) {
		t.Fatalf("answers at stop %+v", got)
	}
	if !errors.Is(refused, ErrClipboardClosed) {
		t.Fatalf("a read while stopping: %v, want ErrClipboardClosed", refused)
	}
	if n := tb.ClipboardReads(); n != 0 {
		t.Fatalf("%d reads still held by the backend after stop", n)
	}

	// a backend with no reader says Closed while stopping too, as the contract promises
	var plainRefused error
	var plainApp *App
	plain := NewTestBackend(10, 2)
	plainApp = NewApp(NewFlex(Vertical), WithBackend(struct{ Backend }{plain}), WithMinFrameInterval(0), WithOnStopping(func() {
		_, plainRefused = plainApp.ReadClipboard(time.Minute, ans.done)
	}))
	runApp(t, plainApp, plain).wait()
	if !errors.Is(plainRefused, ErrClipboardClosed) {
		t.Fatalf("no reader, while stopping: %v, want ErrClipboardClosed", plainRefused)
	}
}

// TestBackend: a cancelled read is dropped from the held ones, and answering finds the next.
func TestTestBackendHeldClipboardReads(t *testing.T) {
	tb := NewTestBackend(1, 1)
	var got []string
	cancel1, _ := tb.ReadClipboard(func(s string, _ error) { got = append(got, "1:"+s) })
	tb.ReadClipboard(func(s string, _ error) { got = append(got, "2:"+s) })
	cancel1()
	cancel1()
	if tb.ClipboardReads() != 1 || !tb.AnswerClipboard("x", nil) || tb.AnswerClipboard("y", nil) {
		t.Fatalf("held %d", tb.ClipboardReads())
	}
	if len(got) != 1 || got[0] != "2:x" {
		t.Fatalf("answered %q", got)
	}
}

// TestBackend: a cancel can't return while an answer is running, so no answer runs after cancel
// returns.
func TestTestBackendCancelWaitsForAnAnswerInProgress(t *testing.T) {
	tb := NewTestBackend(1, 1)
	started, release := make(chan struct{}), make(chan struct{})
	cancel, _ := tb.ReadClipboard(func(string, error) { close(started); <-release })
	go tb.AnswerClipboard("x", nil)
	<-started
	returned := make(chan struct{})
	go func() { cancel(); close(returned) }()
	select {
	case <-returned:
		t.Fatal("cancel returned while the answer was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-returned
}

// ctxProbe keeps its Context, for a widget's view of the App.
type ctxProbe struct {
	Flex
	ctx *Context
}

func (p *ctxProbe) Init(ctx *Context) {
	p.ctx = ctx
	p.Flex.Init(ctx)
}

// A widget reads through its Context as the App reads.
func TestContextReadClipboard(t *testing.T) {
	p := &ctxProbe{Flex: *NewFlex(Vertical)}
	h := startApp(t, p, 10, 2)
	ans := newClipAnswers()
	var err error
	h.onLoop(func() { _, err = p.ctx.ReadClipboard(time.Minute, ans.done) })
	if err != nil {
		t.Fatal(err)
	}
	h.tb.AnswerClipboard("via the context", nil)
	if got := ans.await(t, 1); got[0].text != "via the context" {
		t.Fatalf("answers %+v", got)
	}
}
