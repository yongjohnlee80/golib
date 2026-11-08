package mermaid

import (
	"fmt"
	"strconv"
	"strings"
)

// FRONT MATTER — a Mermaid diagram may open with a YAML block between two --- lines, before its
// type line:
//
//	---
//	title: Release flow
//	config:
//	  theme: forest
//	---
//	flowchart LR
//
// It is read, not refused: its top-level title: (plain, or quoted with " or ', a # comment after
// it) is the diagram's title, which a diagram whose body names one keeps instead; a title that is
// no scalar (a quote left open, an escape YAML doesn't have, text after its closing quote) is a
// SyntaxError at its line, and one longer than MaxLabel is ErrTooLarge, as any label is; config: and every other key are passed
// over, as %%{init}%% directives are, since they style mermaid.js's drawing. Only top-level
// key: value lines are read; an indented line belongs to the key above it. A block with no closing
// --- is a SyntaxError at its opening line.

// frontMatter reads the block whose first line (after its opening ---) starts at byte from, line
// open being the opening ---'s number: it keeps the title, and returns where the line after the
// closing --- starts and how many lines the block took, both --- lines included.
func (p *parser) frontMatter(from, open int) (next, lines int, err error) {
	s := p.src
	i, n := from, 1
	for i < len(s) {
		j := strings.IndexByte(s[i:], '\n')
		end := len(s)
		if j >= 0 {
			end = i + j
		}
		raw := strings.TrimRight(s[i:end], " \t\r")
		n++
		switch {
		case strings.TrimSpace(raw) == "---":
			return end + 1, n, nil
		case raw == "" || raw[0] == ' ' || raw[0] == '\t' || raw[0] == '#':
			// a blank line, a nested key's line, or a comment
		default:
			key, val, ok := strings.Cut(raw, ":")
			if !ok || strings.TrimSpace(key) != "title" {
				break
			}
			line := open + n - 1
			title, ok := yamlScalar(strings.TrimSpace(val))
			switch {
			case !ok:
				return 0, 0, &SyntaxError{Line: line, Col: 1, Msg: fmt.Sprintf("front matter: the title is no YAML scalar: %s", strings.TrimSpace(val))}
			case len(title) > MaxLabel:
				return 0, 0, notDrawn(ErrTooLarge, line, fmt.Sprintf("a label of %d bytes, more than %d", len(title), MaxLabel),
					"a label of %d bytes on line %d, more than %d", len(title), line, MaxLabel)
			}
			p.title = title
		}
		i = end + 1
	}
	return 0, 0, &SyntaxError{Line: open, Col: 1, Msg: "front matter: no closing ---"}
}

// yamlScalar is a YAML flow scalar's text, and whether v is one: a double-quoted scalar with YAML's
// escapes, a single-quoted one with ” for a quote, or a plain one; after a quoted one only spaces
// and a # comment, and a plain one ends where a # comment starts (a # after a space or a tab).
func yamlScalar(v string) (string, bool) {
	switch {
	case strings.HasPrefix(v, `"`):
		return yamlDoubleQuoted(v[1:])
	case strings.HasPrefix(v, "'"):
		var b strings.Builder
		for i := 1; i < len(v); i++ {
			if v[i] != '\'' {
				b.WriteByte(v[i])
				continue
			}
			if i+1 < len(v) && v[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			return b.String(), onlyComment(v[i+1:])
		}
		return "", false
	case strings.HasPrefix(v, "#"):
		return "", true // no value, a comment
	}
	for i := 1; i < len(v); i++ {
		if v[i] == '#' && (v[i-1] == ' ' || v[i-1] == '\t') {
			v = v[:i]
			break
		}
	}
	return strings.TrimSpace(v), true
}

// yamlEscapes are YAML's one-character escapes in a double-quoted scalar.
var yamlEscapes = map[byte]string{'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v",
	'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': `"`, '/': "/", '\\': `\`, 'N': "\u0085", '_': "\u00a0",
	'L': "\u2028", 'P': "\u2029"}

// yamlDoubleQuoted reads a double-quoted scalar's text after its opening quote, to its closing
// quote: its escapes decoded, and only spaces and a # comment after it.
func yamlDoubleQuoted(v string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		switch c := v[i]; c {
		case '"':
			return b.String(), onlyComment(v[i+1:])
		case '\\':
			if i+1 >= len(v) {
				return "", false
			}
			e := v[i+1]
			if s, ok := yamlEscapes[e]; ok {
				b.WriteString(s)
				i++
				continue
			}
			digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
			if digits == 0 || i+2+digits > len(v) {
				return "", false // an escape YAML doesn't have, or a short one
			}
			r, err := strconv.ParseUint(v[i+2:i+2+digits], 16, 32)
			if err != nil {
				return "", false
			}
			b.WriteRune(rune(r))
			i += 1 + digits
		default:
			b.WriteByte(c)
		}
	}
	return "", false // no closing quote
}

// onlyComment reports whether s, what follows a quoted scalar, is spaces and perhaps a # comment.
func onlyComment(s string) bool {
	s = strings.TrimLeft(s, " \t")
	return s == "" || s[0] == '#'
}

// TitleOf is d's title: its body's (title, accTitle aside), else its front matter's; "" for none.
func TitleOf(d Diagram) string {
	if t := titleField(d); t != nil {
		return *t
	}
	return ""
}

// titleField is d's title field, which every diagram type has; nil for another Diagram.
func titleField(d Diagram) *string {
	switch d := d.(type) {
	case *FlowchartDiagram:
		return &d.Title
	case *SequenceDiagram:
		return &d.Title
	case *ClassDiagram:
		return &d.Title
	case *StateDiagram:
		return &d.Title
	case *ERDiagram:
		return &d.Title
	case *GanttDiagram:
		return &d.Title
	case *PieDiagram:
		return &d.Title
	case *QuadrantDiagram:
		return &d.Title
	case *MindmapDiagram:
		return &d.Title
	case *RequirementDiagram:
		return &d.Title
	case *XYChartDiagram:
		return &d.Title
	case *TimelineDiagram:
		return &d.Title
	case *JourneyDiagram:
		return &d.Title
	case *SankeyDiagram:
		return &d.Title
	case *GitGraphDiagram:
		return &d.Title
	case *BlockDiagram:
		return &d.Title
	}
	return nil
}
