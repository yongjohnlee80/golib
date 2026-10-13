package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// VIM REPLACE MODE — R: typed clusters overwrite the text, as Vim's Replace mode has them (its
// replace stack, src/edit.c replace_push, replace_pop and replace_do_bs). Each typed cluster pushes
// what it overwrote, or that it appended past the line's end; Backspace pops: the cluster comes
// back, an appended one goes, a line break typed is joined again. With the stack empty, before
// where this Replace began, Backspace only moves left. Enter breaks the line and overwrites
// nothing. Ctrl-w and Ctrl-u are Backspace over the word or back to the indent, so they restore
// too. An arrow key starts a new stack, as it starts a new Insert step. A count types the text
// again, overwriting, as many times more on Esc (3Rab on 123456789 gives ababab789).
//
// Replace is Insert underneath, with this state: its keys, the escape chord, undo, . and Ctrl-o
// are Insert's. Mode reports ModeReplace while it lasts.

// replaceKind is what a typed cluster did.
type replaceKind uint8

const (
	replOverwrote replaceKind = iota // took the place of orig
	replAppended                     // went past the line's end
	replBreak                        // a line break
)

type replaceEntry struct {
	kind replaceKind
	orig string
}

// replaceState is a Replace session's.
type replaceState struct {
	stack   []replaceEntry
	ln, col int // where it began: the count's text runs from here
	count   int
}

// enterReplace is R, count times.
func (c *EditorCore) enterReplace(count int) {
	b := &c.buf
	c.replace = &replaceState{ln: b.ln, col: b.col, count: max(count, 1)}
	c.enterInsert()
	c.reveal()
	c.markDirty()
}

// replaceText types s over the text: a cluster each, a line break inserted.
func (c *EditorCore) replaceText(s string) {
	b := &c.buf
	r := c.replace
	c.beginGroup()
	for _, cl := range clusters(strings.ReplaceAll(s, "\r\n", "\n")) {
		if cl == "\n" {
			b.insertText("\n")
			r.stack = append(r.stack, replaceEntry{kind: replBreak})
			continue
		}
		cs := b.lineClusters(b.ln)
		if b.col >= len(cs) {
			b.insertText(cl)
			r.stack = append(r.stack, replaceEntry{kind: replAppended})
			continue
		}
		r.stack = append(r.stack, replaceEntry{kind: replOverwrote, orig: cs[b.col]})
		b.lines[b.ln] = strings.Join(cs[:b.col], "") + cl + strings.Join(cs[b.col+1:], "")
		b.touch(b.ln)
		b.col++
	}
	c.edited()
}

// replaceBackspace pops the replace stack: what the last cluster typed overwrote comes back, an
// appended one goes, a break is joined; with none, the cursor only moves left.
func (c *EditorCore) replaceBackspace() {
	b := &c.buf
	r := c.replace
	if len(r.stack) == 0 {
		if b.col > 0 {
			b.col--
			b.desired = -1
			c.markDirty()
		}
		return
	}
	e := r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	c.beginGroup()
	switch e.kind {
	case replBreak:
		if b.col == 0 && b.ln > 0 {
			b.deleteRegion(taPos{b.ln - 1, len(b.lineClusters(b.ln - 1))}, taPos{b.ln, 0})
		}
	case replAppended:
		if b.col > 0 {
			b.deleteRegion(taPos{b.ln, b.col - 1}, taPos{b.ln, b.col})
		}
	default:
		if cs := b.lineClusters(b.ln); b.col > 0 && b.col <= len(cs) {
			b.lines[b.ln] = strings.Join(cs[:b.col-1], "") + e.orig + strings.Join(cs[b.col:], "")
			b.touch(b.ln)
			b.col--
		}
	}
	c.edited()
}

// replaceRestart starts a new stack where the cursor is (an arrow key moved it): Backspace no
// longer goes back past here, and the count is spent.
func (c *EditorCore) replaceRestart() {
	if r := c.replace; r != nil {
		r.stack, r.ln, r.col, r.count = nil, c.buf.ln, c.buf.col, 1
	}
}

// replaceDone is Esc out of Replace: the count's further copies of the text typed, overwriting.
// The text is what this Replace typed on the line it began on.
func (c *EditorCore) replaceDone() {
	r := c.replace
	if r == nil || r.count <= 1 {
		return
	}
	b := &c.buf
	if b.ln != r.ln || b.col <= r.col {
		return
	}
	text := strings.Join(b.lineClusters(b.ln)[r.col:b.col], "")
	for range r.count - 1 {
		c.replaceText(text)
	}
}

// replaceKey is Replace's own keys: Enter breaks the line, Backspace pops the stack, Tab
// overwrites as typed text does; an arrow key starts a new stack, and Insert's handling moves.
func (c *EditorCore) replaceKey(k tui.KeyEvent) bool {
	if k.Mods&nonTextMods != 0 {
		return false
	}
	switch k.Code {
	case tui.KeyEnter:
		c.dotInsertKey(k)
		c.replaceText("\n")
		return true
	case tui.KeyBackspace:
		c.dotInsertKey(k)
		c.replaceBackspace()
		return true
	case tui.KeyTab:
		c.dotInsertKey(k)
		c.replaceText("\t")
		return true
	case tui.KeyLeft, tui.KeyRight, tui.KeyUp, tui.KeyDown, tui.KeyHome, tui.KeyEnd, tui.KeyPageUp, tui.KeyPageDown:
		c.replaceRestart()
	}
	return false
}
