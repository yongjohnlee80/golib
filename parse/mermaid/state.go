package mermaid

import "fmt"

// state parses a state diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) state(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: stateDiagram diagrams", ErrUnsupported)
}
