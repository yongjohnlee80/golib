package mermaid

import (
	"errors"
	"fmt"
)

// Limits on what Parse reads. A diagram past one is ErrTooLarge.
const (
	MaxSource = 64 << 10 // bytes of source
	MaxLabel  = 1 << 10  // bytes of one label, as written
)

var (
	// ErrUnsupported is a diagram golib does not draw natively: another type, or a construct
	// outside the supported subset anywhere in it. The caller hands it to another renderer;
	// nothing is ever drawn half.
	ErrUnsupported = errors.New("mermaid: not drawn natively")
	// ErrTooLarge is a source or a label past its limit.
	ErrTooLarge = errors.New("mermaid: over a limit")
)

// SyntaxError is a malformed construct in the grammar the parser claims. Line and Col are
// 1-based; Col counts characters, not bytes.
type SyntaxError struct {
	Line, Col int
	Msg       string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("mermaid: line %d, column %d: %s", e.Line, e.Col, e.Msg)
}

// Diagram is a parsed diagram; Kind says which model it is.
type Diagram interface{ Kind() Kind }

// Kind is a diagram type.
type Kind uint8

const (
	Flowchart   Kind = iota + 1 // flowchart, graph
	Sequence                    // sequenceDiagram
	State                       // stateDiagram, stateDiagram-v2
	Class                       // classDiagram
	ER                          // erDiagram
	Journey                     // journey
	Mindmap                     // mindmap
	Timeline                    // timeline
	Requirement                 // requirementDiagram
)

var kindNames = [...]string{Flowchart: "flowchart", Sequence: "sequence", State: "state", Class: "class", ER: "er",
	Journey: "journey", Mindmap: "mindmap", Timeline: "timeline", Requirement: "requirement"}

func (k Kind) String() string { return enumName(kindNames[:], int(k)) }

// Dir is the direction a flowchart's ranks run in. The zero Dir is unset: a subgraph without
// its own direction takes its parent's.
type Dir uint8

const (
	TB Dir = iota + 1 // top to bottom (TD is the same)
	BT
	LR
	RL
)

var dirNames = [...]string{TB: "TB", BT: "BT", LR: "LR", RL: "RL"}

func (d Dir) String() string               { return enumName(dirNames[:], int(d)) }
func (d Dir) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Shape is a flowchart node's outline.
type Shape uint8

const (
	Rect             Shape = iota // [text]
	Round                         // (text)
	Stadium                       // ([text])
	Subroutine                    // [[text]]
	Cylinder                      // [(text)]
	Circle                        // ((text))
	Asymmetric                    // >text]
	Diamond                       // {text}
	Hexagon                       // {{text}}
	Parallelogram                 // [/text/]
	ParallelogramAlt              // [\text\]
	Trapezoid                     // [/text\]
	TrapezoidAlt                  // [\text/]
	DoubleCircle                  // (((text)))
)

var shapeNames = [...]string{
	Rect: "rect", Round: "round", Stadium: "stadium", Subroutine: "subroutine", Cylinder: "cylinder",
	Circle: "circle", Asymmetric: "asymmetric", Diamond: "diamond", Hexagon: "hexagon",
	Parallelogram: "parallelogram", ParallelogramAlt: "parallelogram-alt", Trapezoid: "trapezoid",
	TrapezoidAlt: "trapezoid-alt", DoubleCircle: "double-circle",
}

func (s Shape) String() string               { return enumName(shapeNames[:], int(s)) }
func (s Shape) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// LineKind is how an edge's line is drawn.
type LineKind uint8

const (
	Solid     LineKind = iota // --
	Dotted                    // -.-
	Thick                     // ==
	Invisible                 // ~~~: placed, never drawn
)

var lineNames = [...]string{Solid: "solid", Dotted: "dotted", Thick: "thick", Invisible: "invisible"}

func (l LineKind) String() string               { return enumName(lineNames[:], int(l)) }
func (l LineKind) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// Arrow is what an edge ends in.
type Arrow uint8

const (
	None  Arrow = iota
	Head        // > (or < at the From end)
	Dot         // o
	Cross       // x
)

var arrowNames = [...]string{None: "none", Head: "arrow", Dot: "circle", Cross: "cross"}

func (a Arrow) String() string               { return enumName(arrowNames[:], int(a)) }
func (a Arrow) MarshalText() ([]byte, error) { return []byte(a.String()), nil }

func enumName(names []string, i int) string {
	if i >= 0 && i < len(names) && names[i] != "" {
		return names[i]
	}
	return fmt.Sprintf("%d", i)
}

// StyleProp is one CSS-like property of a classDef or a style statement, as written:
// fill, stroke, stroke-width, color, stroke-dasharray, …
type StyleProp struct{ Name, Value string }

// Style is a list of properties; a later one of the same name wins.
type Style []StyleProp

// FlowchartDiagram is a parsed flowchart.
type FlowchartDiagram struct {
	Dir       Dir
	Nodes     []Node // in order of first mention
	Edges     []Edge // in source order; a chain or & gives one edge per pair
	Subgraphs []Subgraph
	Classes   map[string]Style // classDef; "default" applies to every node
}

func (*FlowchartDiagram) Kind() Kind { return Flowchart }

// Node is a flowchart node.
type Node struct {
	ID    string
	Label string // its text, plain: quotes, Markdown marks and entities resolved; the ID when none
	Shape Shape
	// Classes are the node's classes in the order given (class statements and :::).
	Classes []string
	// Style is resolved: classDef default, then each class, then style statements.
	Style Style
	// Subgraph is the innermost subgraph holding the node, "" at top level: the last subgraph
	// it is mentioned in. A mention at top level does not take it out of one.
	Subgraph string
	// Span is the bytes of the mention that gave the node its shape (its first, when none did).
	Span [2]int
}

// Edge is a flowchart edge.
type Edge struct {
	From, To string
	Label    string
	Line     LineKind
	Head     Arrow // at To
	Tail     Arrow // at From
	MinLen   int   // ranks it spans at least; longer arrows ask for more
	Span     [2]int
}

// Subgraph is a box around nodes.
type Subgraph struct {
	ID, Title string
	Dir       Dir    // zero: its parent's
	Parent    string // "" at top level
	Span      [2]int
}
