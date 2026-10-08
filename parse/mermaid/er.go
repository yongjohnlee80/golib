package mermaid

import "fmt"

// er parses an ER diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) er(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: erDiagram diagrams", ErrUnsupported)
}
