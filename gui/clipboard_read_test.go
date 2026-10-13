package gui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/io/input"
	"gioui.org/io/transfer"
	"github.com/yongjohnlee80/golib/tui"
)

// clipRig is a window's input path on Gio's real router: frames read input, and the platform's
// clipboard reply is queued as Gio's window queues it.
type clipRig struct {
	t   *testing.T
	r   *input.Router
	b   *Backend
	mu  sync.Mutex
	got []string // the answers, "<read>:<text>"
}

func newClipRig(t *testing.T) *clipRig {
	g := &clipRig{t: t, r: new(input.Router), b: &Backend{q: newEventQueue()}}
	g.frame() // registers the filters
	return g
}

func (g *clipRig) frame() { g.b.readInput(g.r.Source(), &g.b.gio, metrics{}) }

// read asks for the clipboard as read name; its answers are recorded under that name.
func (g *clipRig) read(name string) (cancel func()) {
	cancel, err := g.b.ReadClipboard(func(text string, err error) {
		g.mu.Lock()
		defer g.mu.Unlock()
		if err != nil {
			text = "error " + err.Error()
		}
		g.got = append(g.got, name+":"+text)
	})
	if err != nil {
		g.t.Fatal(err)
	}
	return cancel
}

// reply is the platform's clipboard reply, untagged, as Gio's window queues it.
func (g *clipRig) reply(text string) {
	g.r.Queue(transfer.DataEvent{Type: textType, Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader(text)) }})
}

func (g *clipRig) answers() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.got...)
}

func (g *clipRig) want(what string, want ...string) {
	g.t.Helper()
	if got := g.answers(); strings.Join(got, "|") != strings.Join(want, "|") {
		g.t.Fatalf("%s: answers %q, want %q", what, got, want)
	}
}

func (g *clipRig) pastes() (n int) {
	for _, ev := range g.b.q.queue {
		if _, ok := ev.(tui.PasteEvent); ok {
			n++
		}
	}
	return n
}

// A read asks the platform on a tag of its own on the next frame, and the reply answers it, never
// as a paste. Two reads asked before a frame are one ReadCmd, both answered.
func TestClipboardReadAsksOnItsOwnTag(t *testing.T) {
	g := newClipRig(t)
	g.read("1")
	g.read("2")
	if g.r.ClipboardRequested() {
		t.Fatal("asked the platform before a frame")
	}
	g.frame()
	if !g.r.ClipboardRequested() {
		t.Fatal("the frame asked the platform nothing")
	}
	g.reply("hello")
	g.frame()
	g.want("one reply", "1:hello", "2:hello")
	if g.pastes() != 0 {
		t.Fatal("a read's reply became a paste")
	}
	g.frame()
	if g.r.ClipboardRequested() {
		t.Fatal("answered reads asked again")
	}

	// a read after the answer is a generation of its own: it asks, and is answered
	g.read("3")
	g.frame()
	if !g.r.ClipboardRequested() {
		t.Fatal("a read after an answered generation asked nothing")
	}
	g.reply("again")
	g.frame()
	g.want("a new read after an answer", "1:hello", "2:hello", "3:again")
}

// A paste's reply goes to the window's tag: a paste, answering no read.
func TestClipboardPasteAnswersNoRead(t *testing.T) {
	g := newClipRig(t)
	g.r.Source().Execute(clipboard.ReadCmd{Tag: &g.b.gio}) // as the paste chord does
	g.reply("pasted")
	g.frame()
	if g.pastes() != 1 || len(g.answers()) != 0 {
		t.Fatalf("pastes %d, answers %q", g.pastes(), g.answers())
	}
}

// A read asked while a generation is in flight joins it: no second ReadCmd, and the one reply
// answers both, once each.
func TestClipboardReadJoinsTheGenerationInFlight(t *testing.T) {
	g := newClipRig(t)
	g.read("1")
	g.frame()
	g.r.ClipboardRequested()
	g.read("2")
	g.frame()
	if g.r.ClipboardRequested() {
		t.Fatal("a read in flight's neighbour asked the platform again")
	}
	g.reply("x")
	g.frame()
	g.frame()
	g.want("one reply for both", "1:x", "2:x")
}

// A read the platform never answers is cancelled (the App's timeout); the next read
// asks again on a new tag, so nothing is stranded; a late reply then answers the new read once, and
// the cancelled read nothing.
func TestClipboardReadAfterATimeoutAsksAgain(t *testing.T) {
	g := newClipRig(t)
	cancel1 := g.read("1")
	g.frame()
	if !g.r.ClipboardRequested() {
		t.Fatal("the first read asked nothing")
	}
	cancel1() // the platform never replied: the App's timeout
	g.read("2")
	g.frame()
	if !g.r.ClipboardRequested() {
		t.Fatal("after a read the platform never answered, the next read asked nothing: stranded")
	}
	g.reply("late")
	g.frame()
	g.frame()
	g.want("the late reply", "2:late")
}

// A cancelled read in a live generation isn't answered, and its neighbour is.
func TestClipboardReadCancelledInALiveGeneration(t *testing.T) {
	g := newClipRig(t)
	cancel1 := g.read("1")
	g.read("2")
	g.frame()
	cancel1()
	cancel1()
	g.reply("y")
	g.frame()
	g.want("after a cancel", "2:y")
}

// A cancel can't return while its read is being answered: no answer after cancel returns.
func TestClipboardReadCancelWaitsForAnAnswerInProgress(t *testing.T) {
	b := &Backend{q: newEventQueue()}
	started, release := make(chan struct{}), make(chan struct{})
	cancel, _ := b.ReadClipboard(func(string, error) { close(started); <-release })
	gen := b.startReadGen()
	go b.answerGen(gen, "x")
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
