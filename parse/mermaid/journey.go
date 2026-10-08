package mermaid

import "fmt"

// journey parses a journey's body: rest is its header line after the keyword, at restOff.
func (p *parser) journey(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: journey diagrams", ErrUnsupported)
}
