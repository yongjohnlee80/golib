package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
)

// BUFFERS — an open document, apart from the editors that show it.
//
// A Buffer is what a document is: its text, the changes logged against it, undo and redo, the marks,
// its revision and saved revision, and what its highlighter made of each line. An EditorCore is how
// one editor edits it: the cursor, the mode, the keys, the find, the layout. An editor shows one
// buffer at a time (SetBuffer) and keeps its own cursor in each buffer it has shown, as a place.
//
//	Buffer ──► bufferState ◄──────── EditorCore (embeds the shown buffer's, by pointer)
//	             ├── text *bufferText ◄── EditorCore.buf (a textBuffer: a cursor over it)
//	             ├── hist, marks, rev, savedRev, revs, hlFrom, source, sourceFactory, jumpDoc
//	             ├── hlText           ◄── EditorCore.hl.text (the editor paints its own styles)
//	             ├── editors          the editors that show it now
//	             └── places           where each editor that has shown it left its cursor
//
// bufferState and bufferText are data only, with no methods: an EditorCore embeds the shown
// buffer's bufferState, and its textBuffer embeds the text, so c.hist, c.marks, c.rev and c.buf.lines
// read as they did when the core held them itself, and no method is ever promoted.
//
// An edit made through any editor is logged once in the buffer, and followEdits drains it once:
// the marks, the jump list, the cursors of the other editors showing the buffer and the places of
// the editors that are not, all follow it by one rule (followPos).
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned, as the editors that show it.

// bufferState is everything a document keeps besides its lines' cursor: shared by every editor that
// shows the buffer.
type bufferState struct {
	handle *Buffer // the one Buffer that is this state's: what EditorCore.Buffer returns
	text   *bufferText
	hist   editHistory // undo/redo
	// marks is a–z and the previous jump's place: Vim's are per buffer
	marks markSet
	// rev is the text's revision, a fresh value at every edit (revs counts them out, per buffer),
	// and savedRev the revision MarkSaved recorded: Modified compares the two.
	rev, savedRev, revs uint64
	// hlFrom is the first line changed that the highlighter has not looked at yet: an edit takes
	// the buffer's change mark for the layout and leaves it here (takeHighlightChanged).
	hlFrom        int
	hlText        hlText // what the highlighter made of each line (editor_highlight.go)
	sourceFactory func() highlight.Source
	source        highlight.Source
	jumpDoc       string // the document as the jump list knows it (SetJumpDoc, Buffer.SetName)
	// editors are the editors that show the buffer now; places, where each editor that has shown
	// it left its cursor when it switched away
	editors []*EditorCore
	places  map[*EditorCore]place
}

// place is an editor's cursor in a buffer it is not showing: restored by SetBuffer, moved by edits.
type place struct {
	at      taPos
	desired int // the sticky column, -1 unset
	top     int // the first line the editor showed (EditorViewport.VisibleLines); 0 without one
}

// newBufferState is an empty document: one empty line, undo on, its edits logged.
func newBufferState() *bufferState {
	s := &bufferState{
		text:   newBufferText(),
		hist:   editHistory{enabled: true},
		hlFrom: noChange,
		places: map[*EditorCore]place{},
	}
	s.text.log.on = true // the marks and the other editors' cursors follow the line structure's changes
	return s
}

// Buffer is an open document. A host keeps one per open file; an EditorCore shows one at a time
// (SetBuffer), and a core with no widget edits one no editor shows by the same code a user's keys
// run.
type Buffer struct{ s *bufferState }

// newBuffer is an empty document and its handle.
func newBuffer() *Buffer {
	b := &Buffer{s: newBufferState()}
	b.s.handle = b
	return b
}

// BufferOption configures a Buffer under construction.
type BufferOption func(*Buffer)

// NewBuffer is an empty document, or as its options make it.
func NewBuffer(opts ...BufferOption) *Buffer {
	b := newBuffer()
	for _, o := range opts {
		if o != nil {
			o(b)
		}
	}
	return b
}

// BufferText seeds the buffer's text, normalizing newlines: unmodified, with no undo history.
func BufferText(text string) BufferOption {
	return func(b *Buffer) {
		cur := textBuffer{bufferText: b.s.text}
		cur.setValue(text)
		b.s.text.log.take() // a document's first text is no edit for anything to follow
	}
}

// BufferSourceFactory makes the buffer's highlighting source, as CoreSourceFactory does an editor's.
func BufferSourceFactory(factory func() highlight.Source) BufferOption {
	return func(b *Buffer) {
		b.s.sourceFactory = factory
		source := highlight.Source{}
		if factory != nil {
			source = factory()
		}
		b.s.hlText.setSource(source)
		b.s.source = source
	}
}

// BufferName names the buffer as the jump list knows it (Buffer.SetName).
func BufferName(doc string) BufferOption { return func(b *Buffer) { b.s.jumpDoc = doc } }

// Value returns the text, lines joined with newlines.
func (b *Buffer) Value() string { return strings.Join(b.s.text.lines, "\n") }

// Modified reports whether the text differs from the saved revision (MarkSaved).
func (b *Buffer) Modified() bool { return b.s.rev != b.s.savedRev }

// MarkSaved records the current text as saved.
func (b *Buffer) MarkSaved() { b.s.savedRev = b.s.rev }

// Revision is the text's revision: a new value at every edit, an undo back to a revision its own
// again. Revisions are a buffer's own; two buffers may share a value.
func (b *Buffer) Revision() uint64 { return b.s.rev }

// SetName names the buffer as the jump list knows it: what an editor showing it records in each
// jump (JumpPos.Doc), and what a jump to another document asks the host to show.
func (b *Buffer) SetName(doc string) { b.s.jumpDoc = doc }

// Shown reports whether an editor shows the buffer now.
func (b *Buffer) Shown() bool { return len(b.s.editors) > 0 }

// --- following an edit -------------------------------------------------------------

// followPos moves p with one change to the line structure. gone reports that p's line was removed
// (or the document replaced): a mark is then erased, and a cursor moves instead (followCursorPos).
func followPos(p *taPos, e lineEdit) (gone bool) {
	switch {
	case e.reset:
		return true
	case e.move:
		if p.ln == e.from && (e.all || p.col >= e.cut) {
			p.ln, p.col = e.to, e.at+max(p.col-e.cut, 0)
		}
	case p.ln >= e.ln && p.ln < e.ln+e.del:
		return true
	case p.ln >= e.ln+e.del:
		p.ln += e.ins - e.del
	}
	return false
}

// followCursorPos moves a cursor-like position with one change: as followPos, except that a cursor
// on a removed line goes to the start of the first line after the change (the line that took the
// removed lines' place), and a replaced document puts it at the start.
func followCursorPos(p *taPos, e lineEdit) {
	if !followPos(p, e) {
		return
	}
	if e.reset {
		*p = taPos{}
		return
	}
	p.ln, p.col = e.ln+e.ins, 0 // clamped into the text by whoever shows it next
}

// followEditor moves another editor's cursor, its Visual anchor and its first shown line with a
// change it did not make.
func followEditor(o *EditorCore, e lineEdit) {
	at := taPos{o.buf.ln, o.buf.col}
	followCursorPos(&at, e)
	o.buf.ln, o.buf.col = at.ln, at.col
	if o.buf.anchor != nil {
		followCursorPos(o.buf.anchor, e)
	}
	followCursorPos(&o.vAnchor, e)
	if v, ok := o.layout.(EditorViewport); ok {
		first, _ := v.VisibleLines()
		top := taPos{ln: first}
		followCursorPos(&top, e)
		if d := top.ln - first; d != 0 {
			v.ScrollLines(d) // the same text stays at the top of the view
		}
	}
}

// followPlace moves an editor's place in a buffer it is not showing.
func followPlace(p place, e lineEdit) place {
	followCursorPos(&p.at, e)
	top := taPos{ln: p.top}
	followCursorPos(&top, e)
	p.top = top.ln
	return p
}
