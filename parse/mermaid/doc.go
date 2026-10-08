// Package mermaid parses Mermaid diagrams into models a native renderer draws: flowcharts
// (flowchart and graph), sequence, class, state and ER diagrams.
//
//	d, err := mermaid.Parse("flowchart LR\n  A[Start] --> B{Ready?}\n  B -->|yes| C([Done])")
//	switch {
//	case errors.Is(err, mermaid.ErrUnsupported), errors.Is(err, mermaid.ErrTooLarge):
//		// another renderer draws it
//	case err != nil:
//		// a *SyntaxError: show it, with its line
//	}
//	fc := d.(*mermaid.FlowchartDiagram)
//
// The parser reads the whole diagram before it answers. One construct outside the supported
// subset anywhere makes the whole diagram ErrUnsupported, so a renderer never draws part of a
// diagram as if it were all of it. A SyntaxError is reserved for a malformed construct inside the
// grammar the parser claims; when a diagram has both, ErrUnsupported wins, since the other
// renderer may read what this parser cannot.
//
// The flowchart subset: every node shape of the classic syntax; solid, dotted, thick and
// invisible links of any length, with arrow, circle and cross ends on either side; link text in
// both forms; chains and &; subgraphs with a title and their own direction; classDef, class,
// ::: and style; %% comments; quoted labels, Markdown-string labels (as plain text), entity
// codes and <br>. Outside it, and so ErrUnsupported: click, linkStyle, Font Awesome icons, the
// @{ } shape and edge-id syntax, front matter, links to a subgraph, and other HTML in labels.
// Each other type has its own subset, in its file; a type with none is ErrUnsupported.
//
// Standard library only.
package mermaid
