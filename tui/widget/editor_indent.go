package widget

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"strings"
)

func (c *EditorCore) indentDecision(op indent.Operation, column int) indent.Decision {
	line := c.buf.lines[c.buf.ln]
	d := indent.Decision{}
	if !c.autoIndent || c.readOnly {
		return d
	}
	d.Prefix = indent.Leading(line)
	if c.source.Indenter == nil {
		return d
	}
	known, incoming := c.buf.ln == 0, highlight.State(0)
	valid := c.hl.verifiedFrom(c.buf.lines, c.takeHighlightChanged)
	if c.buf.ln > 0 && valid >= c.buf.ln && len(c.hl.lines) >= c.buf.ln {
		known, incoming = true, c.hl.lines[c.buf.ln-1].out
	}
	if c.source.States != nil {
		c.source.States.Retain(incoming)
		defer c.source.States.Release(incoming)
	}
	r := indent.Request{Line: line, Column: column, Operation: op, Unit: c.indentUnit,
		PreviousState: int(incoming), StateKnown: known}
	if got, ok := c.source.Indenter.Indent(r); ok && indent.Whitespace(got.Prefix) && indent.Whitespace(got.ClosingPrefix) {
		return got
	}
	return d
}

func (c *EditorCore) byteColumn() int {
	cs := c.buf.lineClusters(c.buf.ln)
	return len(strings.Join(cs[:min(c.buf.col, len(cs))], ""))
}

func (c *EditorCore) insertIndentedNewline() {
	b := &c.buf
	c.alignClosing()
	d := c.indentDecision(indent.Newline, c.byteColumn())
	text := "\n" + d.Prefix
	if d.SplitClosing {
		right := b.lines[b.ln][c.byteColumn():]
		spaces := len(indent.Leading(right))
		if spaces > 0 {
			b.deleteRegion(taPos{b.ln, b.col}, taPos{b.ln, b.col + spaces})
		}
		text += "\n" + d.ClosingPrefix
	}
	b.insertText(text)
	if d.SplitClosing {
		b.ln--
		b.col = len(clusters(d.Prefix))
		b.desired = -1
	}
}

func (c *EditorCore) alignClosing() {
	if !c.autoIndent || c.readOnly || c.source.Indenter == nil {
		return
	}
	b := &c.buf
	old := indent.Leading(b.lines[b.ln])
	d := c.indentDecision(indent.Closing, c.byteColumn())
	if old == d.Prefix {
		return
	}
	b.lines[b.ln] = d.Prefix + b.lines[b.ln][len(old):]
	b.col = max(0, b.col+len(clusters(d.Prefix))-len(clusters(old)))
	b.touch(b.ln)
}
