package mermaid

import "fmt"

// timeline parses a timeline's body: rest is its header line after the keyword, at restOff.
func (p *parser) timeline(rest string, restOff int) (Diagram, error) {
	return nil, fmt.Errorf("%w: timeline diagrams", ErrUnsupported)
}
