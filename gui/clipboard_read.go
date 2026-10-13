package gui

import (
	"io"
	"slices"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/transfer"
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
// runs under b.mu, so a cancel never returns while its read is being answered (an App's answer only
// enqueues an Update).

// clipRead is one read: what answers it.
type clipRead struct{ answer func(text string, err error) }

// readGen is one generation of reads; its pointer is its Gio tag.
type readGen struct{ reads []*clipRead }

// ReadClipboard implements tui.ClipboardReader: the read waits for the window's next frame to ask
// the platform, or joins the generation already asking.
func (b *Backend) ReadClipboard(answer func(text string, err error)) (cancel func(), err error) {
	r := &clipRead{answer: answer}
	b.mu.Lock()
	ask := b.gen == nil
	if ask {
		b.reads = append(b.reads, r)
	} else {
		b.gen.reads = append(b.gen.reads, r)
	}
	b.mu.Unlock()
	if ask && b.win != nil {
		b.win.Invalidate()
	}
	return func() { b.cancelRead(r) }, nil
}

// cancelRead withdraws r; a generation it leaves with no read is abandoned.
func (b *Backend) cancelRead(r *clipRead) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reads = slices.DeleteFunc(b.reads, func(x *clipRead) bool { return x == r })
	if g := b.gen; g != nil {
		g.reads = slices.DeleteFunc(g.reads, func(x *clipRead) bool { return x == r })
		if len(g.reads) == 0 {
			b.gen = nil
		}
	}
}

// startReadGen makes the reads waiting a generation in flight, and returns it; nil when one is in
// flight already or none waits. The Gio goroutine executes its ReadCmd.
func (b *Backend) startReadGen() *readGen {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gen != nil || len(b.reads) == 0 {
		return nil
	}
	b.gen = &readGen{reads: b.reads}
	b.reads = nil
	return b.gen
}

// inFlight is the generation asking the platform, nil for none.
func (b *Backend) inFlight() *readGen {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gen
}

// answerGen answers generation g's reads with text and ends it, unless it was abandoned meanwhile.
func (b *Backend) answerGen(g *readGen, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gen != g {
		return
	}
	b.gen = nil
	for _, r := range g.reads {
		r.answer(text, nil)
	}
}

// readClipboardReplies takes the platform's reply for the generation in flight, then starts the
// next generation for the reads waiting: the second event pass of a frame, after the window's own
// (a data event there is a paste), since a data event names no target.
func (b *Backend) readClipboardReplies(src eventSource) {
	if g := b.inFlight(); g != nil {
		for {
			ev, ok := src.Event(transfer.TargetFilter{Target: event.Tag(g), Type: textType})
			if !ok {
				break
			}
			if de, ok := ev.(transfer.DataEvent); ok {
				text, _ := transferText(de) // no data: an empty clipboard, ""
				b.answerGen(g, text)
			}
		}
	}
	if g := b.startReadGen(); g != nil {
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
