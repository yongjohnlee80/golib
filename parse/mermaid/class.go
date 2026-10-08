package mermaid

import "fmt"

// class parses a class diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) class(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: classDiagram diagrams", ErrUnsupported)
}
