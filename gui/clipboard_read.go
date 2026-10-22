package gui

import (
	"io"
	"slices"
	"sync"

	"github.com/yongjohnlee80/gio/io/clipboard"
	"github.com/yongjohnlee80/gio/io/event"
	"github.com/yongjohnlee80/gio/io/transfer"
)

// CLIPBOARD READS — tui.ClipboardReader for the window. A read is answered by a generation: one
// clipboard.ReadCmd with a tag of its own, the generation's pointer, never the window's tag (the
// paste chord's), so a read never takes a paste's reply away. A generation takes every read waiting
// when the Gio goroutine starts it; a read asked while one is in flight joins it, and is answered by
// the first platform reply after it was asked. Gio's reply carries no tag: it goes to every tag
// waiting, so one platform reply can answer a newer generation than the request it replied to,
// with the clipboard's text all the same.
//
// A cancel (the App's timeout, or its stop) withdraws its read; a generation left with no read is
// abandoned, and the next read starts a new one with a new tag, so a platform that never replied
// strands nothing: Gio asks the platform again only for a tag not already waiting. Every answer
// runs under the bridge's lock, so a cancel never returns while its read is being answered (an
// App's answer only enqueues an Update).

// clipboardBridge is the window's clipboard between the tui loop and the Gio goroutine: a write
// waiting for the next frame (WriteClipboard), and the reads waiting or in flight. Its lock guards
// all of it, and nothing else.
type clipboardBridge struct {
	mu    sync.Mutex
	write *string     // a write for the Gio goroutine to perform; nil for none
	reads []*clipRead // reads waiting for the next generation
	gen   *readGen    // the generation in flight, nil for none: its pointer is its Gio tag
}

// put leaves s for the Gio goroutine to write.
func (c *clipboardBridge) put(s string) {
	c.mu.Lock()
	c.write = &s
	c.mu.Unlock()
}

// take is the write waiting, nil for none, and leaves none.
func (c *clipboardBridge) take() *string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.write
	c.write = nil
	return s
}

// clipRead is one read: what answers it.
type clipRead struct{ answer func(text string, err error) }

// readGen is one generation of reads; its pointer is its Gio tag.
type readGen struct{ reads []*clipRead }

// ReadClipboard implements tui.ClipboardReader: the read waits for the window's next frame to ask
// the platform, or joins the generation already asking.
func (b *Backend) ReadClipboard(answer func(text string, err error)) (cancel func(), err error) {
	r := &clipRead{answer: answer}
	if b.clip.ask(r) && b.win != nil {
		b.win.Invalidate()
	}
	return func() { b.clip.cancel(r) }, nil
}

// ask adds r to the reads waiting, reporting whether it waits for a new generation (the window
// must be asked for a frame), or joins the one in flight.
func (c *clipboardBridge) ask(r *clipRead) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != nil {
		c.gen.reads = append(c.gen.reads, r)
		return false
	}
	c.reads = append(c.reads, r)
	return true
}

// cancel withdraws r; a generation it leaves with no read is abandoned.
func (c *clipboardBridge) cancel(r *clipRead) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads = slices.DeleteFunc(c.reads, func(x *clipRead) bool { return x == r })
	if g := c.gen; g != nil {
		g.reads = slices.DeleteFunc(g.reads, func(x *clipRead) bool { return x == r })
		if len(g.reads) == 0 {
			c.gen = nil
		}
	}
}

// startGen makes the reads waiting a generation in flight, and returns it; nil when one is in
// flight already or none waits. The Gio goroutine executes its ReadCmd.
func (c *clipboardBridge) startGen() *readGen {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != nil || len(c.reads) == 0 {
		return nil
	}
	c.gen = &readGen{reads: c.reads}
	c.reads = nil
	return c.gen
}

// inFlight is the generation asking the platform, nil for none.
func (c *clipboardBridge) inFlight() *readGen {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// answer answers generation g's reads with text and ends it, unless it was abandoned meanwhile.
func (c *clipboardBridge) answer(g *readGen, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != g {
		return
	}
	c.gen = nil
	for _, r := range g.reads {
		r.answer(text, nil)
	}
}

// readClipboardReplies takes the platform's reply for the generation in flight, then starts the
// next generation for the reads waiting: the second event pass of a frame, after the window's own
// (a data event there is a paste), since a data event names no target.
func (b *Backend) readClipboardReplies(src eventSource) {
	if g := b.clip.inFlight(); g != nil {
		for {
			ev, ok := src.Event(transfer.TargetFilter{Target: event.Tag(g), Type: textType})
			if !ok {
				break
			}
			if de, ok := ev.(transfer.DataEvent); ok {
				text, _ := transferText(de) // no data: an empty clipboard, ""
				b.clip.answer(g, text)
			}
		}
	}
	if g := b.clip.startGen(); g != nil {
		src.Execute(clipboard.ReadCmd{Tag: event.Tag(g)})
	}
}

// transferText is a data event's text; ok is false when it has no data to open.
func transferText(e transfer.DataEvent) (text string, ok bool) {
	r := e.Open()
	if r == nil {
		return "", false
	}
	defer r.Close()
	data, _ := io.ReadAll(r)
	return string(data), true
}
