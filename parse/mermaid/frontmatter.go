package mermaid

import (
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
// It is read, not refused: its top-level title: (plain, or quoted with " or ') is the diagram's
// title, which a diagram whose body names one keeps instead; config: and every other key are passed
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
			if ok && strings.TrimSpace(key) == "title" {
				p.title = yamlScalar(strings.TrimSpace(val))
			}
		}
		i = end + 1
	}
	return 0, 0, &SyntaxError{Line: open, Col: 1, Msg: "front matter: no closing ---"}
}

// yamlScalar is a YAML scalar's text: a double-quoted one unescaped, a single-quoted one with its
// doubled quotes made single, a plain one up to a comment.
func yamlScalar(v string) string {
	switch {
	case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
		if u, err := strconv.Unquote(v); err == nil {
			return u
		}
		return v[1 : len(v)-1]
	case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	if k := strings.Index(v, " #"); k >= 0 {
		v = strings.TrimSpace(v[:k])
	}
	return v
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
