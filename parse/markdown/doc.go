// Package markdown parses CommonMark 0.31.2 into a tree of nodes that point back into the source.
//
// [Parse] runs the specification's two phases. The block phase reads the source a line at a time
// against the stack of open blocks, and collects link reference definitions as paragraphs close.
// The inline phase then reads each paragraph, heading and table cell, pairing emphasis delimiters
// and resolving links against every definition, wherever in the document it was written.
//
//	doc := markdown.Parse(src, markdown.GFM(), markdown.Obsidian())
//	for n := doc.Root.FirstChild; n != nil; n = n.Next {
//		pos := doc.Position(n.Span.Start) // line and column, on demand
//		_ = pos
//	}
//
// # The tree
//
// Every node has a [Kind] and a [Span], a byte range of [Document.Source]; a node's span lies
// inside its parent's, and siblings' spans are ordered and disjoint. Text is not copied: a text
// node's text is the source it spans, and [Node.Literal] is set only where it differs (escapes,
// entity references, a normalized code span, a replaced NUL). Link reference definitions are
// taken out of their paragraphs into [Document.Refs], as the specification says, and also stay in
// the tree as KindLinkRefDef nodes where they were written, so an editor or indexer can find them;
// renderers skip them.
//
// Parse takes any bytes and never fails: any sequence of characters is a CommonMark document.
// Invalid UTF-8 is an ordinary byte to both phases and is kept as it is.
//
// # Extensions
//
// Without options the parser is CommonMark exactly. [GFM] adds GitHub Flavored Markdown 0.29
// (tables, task list items, strikethrough, extended autolinks); [Obsidian] adds frontmatter,
// wikilinks, embeds, tags and callouts. Both hook into the two phases rather than fork them.
// An extension re-reads some valid CommonMark (a pipe table was a paragraph), so what is promised
// is narrower: on a source its [Extension.Recognizes] rejects, the tree is the same with the
// extension as without it.
//
// # Rendering
//
// The html subpackage writes a document as HTML in the specification's reference form; it is the
// form the conformance cases compare.
package markdown
