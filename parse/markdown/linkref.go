package markdown

import (
	"bytes"

	"golang.org/x/text/cases"
)

var folder = cases.Fold()

// normalizeLabel is how labels are matched: case-folded (Unicode full folding), with leading and
// trailing whitespace dropped and internal runs of whitespace collapsed to one space.
//
// https://spec.commonmark.org/0.31.2/#matches
func normalizeLabel(label []byte) string {
	fields := bytes.Fields(label)
	return folder.String(string(bytes.Join(fields, []byte{' '})))
}

// finalizeParagraph takes the paragraph's leading link reference definitions out into the document's
// map, leaving provenance nodes where they were written, and removes the paragraph if nothing is left.
func (p *blockParser) finalizeParagraph(para *Node) {
	p.extractLinkRefs(para)
	if len(bytes.TrimSpace(para.blk.content)) == 0 {
		para.Unlink()
	}
}

// extractLinkRefs consumes definitions from the start of para's content, one per iteration.
func (p *blockParser) extractLinkRefs(para *Node) {
	b := para.blk
	for {
		n, label, dest, title, ok := parseLinkRefDef(b.content)
		if !ok {
			return
		}
		key := normalizeLabel(label)
		if _, dup := p.doc.Refs[key]; !dup {
			p.doc.Refs[key] = LinkRef{Dest: dest, Title: title}
		}
		start := mapOffset(b.segs, 0)
		end := mapOffset(b.segs, n)
		def := &Node{Kind: KindLinkRefDef, Span: Span{start, max(start, trimEnd(p.src, end))},
			Label: label, Dest: dest, Title: title, blk: &blockState{}} // a closed block, like any other
		para.InsertBefore(def)
		b.content = b.content[n:]
		b.segs = shiftSegs(b.segs, n)
		if len(b.segs) > 0 {
			para.Span.Start = mapOffset(b.segs, 0)
		}
	}
}

// trimEnd backs an offset off any line ending before it.
func trimEnd(src []byte, end int) int {
	for end > 0 && end <= len(src) && isLineEnd(src[end-1]) {
		end--
	}
	return end
}

// mapOffset turns an offset into assembled content back into a source offset.
func mapOffset(segs []seg, off int) int {
	for i, s := range segs {
		lineEnd := s.at + s.pad + s.n // the '\n' that stands for the line's end
		if off <= lineEnd || i == len(segs)-1 {
			rel := off - s.at - s.pad
			if rel < 0 {
				rel = 0
			}
			if rel > s.n {
				rel = s.n
			}
			return s.src + rel
		}
	}
	return 0
}

// shiftSegs drops the first n bytes of content from the segment list.
func shiftSegs(segs []seg, n int) []seg {
	var out []seg
	for _, s := range segs {
		lineEnd := s.at + s.pad + s.n + 1
		if lineEnd <= n {
			continue
		}
		if s.at < n {
			cut := n - s.at
			if cut <= s.pad {
				s.pad -= cut
			} else {
				c := cut - s.pad
				s.pad = 0
				s.src += c
				s.n -= c
			}
			s.at = n
		}
		s.at -= n
		out = append(out, s)
	}
	return out
}

// parseLinkRefDef parses one link reference definition at the start of b, returning how many bytes
// it used, including its line ending.
//
// https://spec.commonmark.org/0.31.2/#link-reference-definitions
func parseLinkRefDef(b []byte) (n int, label, dest, title []byte, ok bool) {
	i := 0
	for i < len(b) && i < 3 && b[i] == ' ' {
		i++
	}
	lab, j, ok := scanLinkLabel(b, i)
	if !ok || j >= len(b) || b[j] != ':' {
		return 0, nil, nil, nil, false
	}
	i = skipSpaceAtMostOneLine(b, j+1)
	d, k, ok := scanLinkDest(b, i)
	if !ok {
		return 0, nil, nil, nil, false
	}
	// a title, if any, must be separated from the destination by whitespace
	afterDest := k
	t := skipSpaceAtMostOneLine(b, afterDest)
	if t > afterDest {
		if ttl, m, ok := scanLinkTitle(b, t); ok {
			if e, ok := restOfLineBlank(b, m); ok {
				return e, lab, unescapeAll(d), unescapeAll(ttl), true
			}
		}
	}
	// no title (or one followed by other text): the destination must end its line
	if e, ok := restOfLineBlank(b, afterDest); ok {
		return e, lab, unescapeAll(d), nil, true
	}
	return 0, nil, nil, nil, false
}

// restOfLineBlank reports whether only spaces and tabs remain on the line at i, and returns the
// offset just past its line ending.
func restOfLineBlank(b []byte, i int) (int, bool) {
	for i < len(b) && isSpaceOrTab(b[i]) {
		i++
	}
	if i == len(b) {
		return i, true
	}
	if b[i] == '\n' {
		return i + 1, true
	}
	return 0, false
}

func skipSpaceAtMostOneLine(b []byte, i int) int {
	for i < len(b) && isSpaceOrTab(b[i]) {
		i++
	}
	if i < len(b) && b[i] == '\n' {
		i++
		for i < len(b) && isSpaceOrTab(b[i]) {
			i++
		}
	}
	return i
}

// scanLinkLabel scans "[label]" at i: at most 999 characters, no unescaped brackets, and at least one
// character that is not whitespace.
//
// https://spec.commonmark.org/0.31.2/#link-label
func scanLinkLabel(b []byte, i int) ([]byte, int, bool) {
	if i >= len(b) || b[i] != '[' {
		return nil, 0, false
	}
	j := i + 1
	nonSpace := false
	for j < len(b) {
		switch c := b[j]; {
		case c == '\\' && j+1 < len(b) && isASCIIPunct(b[j+1]):
			j += 2
			nonSpace = true
			continue
		case c == '[':
			return nil, 0, false
		case c == ']':
			if !nonSpace || j-(i+1) > 999 {
				return nil, 0, false
			}
			return b[i+1 : j], j + 1, true
		case c != ' ' && c != '\t' && c != '\n':
			nonSpace = true
		}
		j++
		if j-(i+1) > 999 {
			return nil, 0, false
		}
	}
	return nil, 0, false
}

// scanLinkDest scans a link destination at i: "<...>" with no line ending or unescaped '<' or '>', or
// a nonempty run with no control characters or spaces and only balanced unescaped parentheses.
//
// https://spec.commonmark.org/0.31.2/#link-destination
func scanLinkDest(b []byte, i int) ([]byte, int, bool) {
	if i < len(b) && b[i] == '<' {
		for j := i + 1; j < len(b); j++ {
			switch b[j] {
			case '\\':
				if j+1 < len(b) && isASCIIPunct(b[j+1]) {
					j++
				}
			case '\n', '<':
				return nil, 0, false
			case '>':
				return b[i+1 : j], j + 1, true
			}
		}
		return nil, 0, false
	}
	depth := 0
	j := i
	for j < len(b) {
		c := b[j]
		switch {
		case c == '\\' && j+1 < len(b) && isASCIIPunct(b[j+1]):
			j += 2
			continue
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				return b[i:j], j, j > i
			}
			depth--
		case c <= ' ' || c == 0x7f:
			if depth != 0 {
				return nil, 0, false
			}
			return b[i:j], j, j > i
		}
		j++
	}
	if depth != 0 {
		return nil, 0, false
	}
	return b[i:j], j, j > i
}

// scanLinkTitle scans a quoted or parenthesized title at i. It may span lines but not a blank line.
//
// https://spec.commonmark.org/0.31.2/#link-title
func scanLinkTitle(b []byte, i int) ([]byte, int, bool) {
	if i >= len(b) {
		return nil, 0, false
	}
	var closer byte
	switch b[i] {
	case '"':
		closer = '"'
	case '\'':
		closer = '\''
	case '(':
		closer = ')'
	default:
		return nil, 0, false
	}
	for j := i + 1; j < len(b); j++ {
		switch c := b[j]; {
		case c == '\\' && j+1 < len(b) && isASCIIPunct(b[j+1]):
			j++
		case c == closer:
			return b[i+1 : j], j + 1, true
		case closer == ')' && c == '(':
			return nil, 0, false
		case c == '\n':
			// a blank line ends the attempt
			k := j + 1
			for k < len(b) && isSpaceOrTab(b[k]) {
				k++
			}
			if k >= len(b) || b[k] == '\n' {
				return nil, 0, false
			}
		}
	}
	return nil, 0, false
}
