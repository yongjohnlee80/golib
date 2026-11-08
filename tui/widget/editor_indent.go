package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

func (c *EditorCore) indentDecision(op indent.Operation, column int) indent.Decision {
	line := c.buf.Lines[c.buf.Ln]
	d := indent.Decision{}
	if !c.autoIndent || c.readOnly {
		return d
	}
	d.Prefix = indent.Leading(line)
	if c.source.Indenter == nil {
		return d
	}
	known, incoming := c.buf.Ln == 0, highlight.State(0)
	valid := c.hl.text.verifiedFrom(c.buf.Lines, c.takeHighlightChanged)
	if c.buf.Ln > 0 && valid >= c.buf.Ln && len(c.hl.text.lines) >= c.buf.Ln {
		known, incoming = true, c.hl.text.lines[c.buf.Ln-1].out
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
	cs := c.buf.LineClusters(c.buf.Ln)
	return len(strings.Join(cs[:min(c.buf.Col, len(cs))], ""))
}

func (c *EditorCore) insertIndentedNewline() {
	b := &c.buf
	c.alignClosing()
	d := c.indentDecision(indent.Newline, c.byteColumn())
	text := "\n" + d.Prefix
	if d.SplitClosing {
		right := b.Lines[b.Ln][c.byteColumn():]
		spaces := len(indent.Leading(right))
		if spaces > 0 {
			b.DeleteRegion(txt.Pos{Ln: b.Ln, Col: b.Col}, txt.Pos{Ln: b.Ln, Col: b.Col + spaces})
		}
		text += "\n" + d.ClosingPrefix
	}
	b.InsertText(text)
	if d.SplitClosing {
		b.Ln--
		b.Col = len(txt.Clusters(d.Prefix))
		b.Desired = -1
	}
}

func (c *EditorCore) alignClosing() {
	if !c.autoIndent || c.readOnly || c.source.Indenter == nil {
		return
	}
	b := &c.buf
	old := indent.Leading(b.Lines[b.Ln])
	d := c.indentDecision(indent.Closing, c.byteColumn())
	if old == d.Prefix {
		return
	}
	b.SetLine(b.Ln, d.Prefix+b.Lines[b.Ln][len(old):])
	b.Col = max(0, b.Col+len(txt.Clusters(d.Prefix))-len(txt.Clusters(old)))
}
