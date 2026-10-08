package mermaid

import "fmt"

// mindmap parses a mindmap's body: rest is its header line after the keyword, at restOff.
func (p *parser) mindmap(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: mindmap diagrams", ErrUnsupported)
}
