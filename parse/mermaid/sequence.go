package mermaid

import "fmt"

// sequence parses a sequence diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) sequence(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: sequenceDiagram diagrams", ErrUnsupported)
}
