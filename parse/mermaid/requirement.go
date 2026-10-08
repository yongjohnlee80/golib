package mermaid

import "fmt"

// requirement parses a requirement diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) requirement(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: requirementDiagram diagrams", ErrUnsupported)
}
