# parse/mermaid

Parses Mermaid diagrams into models a native renderer draws. Today: flowcharts (`flowchart` and
`graph`). Sequence, state, class and ER diagrams have their `Kind`s and are answered
`ErrUnsupported` until their parsers land. Standard library only.

```go
import "github.com/yongjohnlee80/golib/parse/mermaid"

d, err := mermaid.Parse(src)
switch {
case errors.Is(err, mermaid.ErrUnsupported), errors.Is(err, mermaid.ErrTooLarge):
	// hand it to another renderer
case err != nil:
	var se *mermaid.SyntaxError // its Line and Col, to show in place
}
fc := d.(*mermaid.FlowchartDiagram) // Dir, Nodes, Edges, Subgraphs, Classes
```

## The answer is all or nothing

`Parse` reads the whole diagram before it answers. If one construct anywhere is outside the
supported subset, the whole diagram is `ErrUnsupported`, so a renderer never draws part of a
diagram as if it were all of it. `SyntaxError` is for a malformed construct inside the grammar
this parser claims. When a diagram has both, `ErrUnsupported` wins, because the other renderer
may read what this one cannot.

A source over 64 KiB (`MaxSource`), or a label over 1 KiB (`MaxLabel`), is `ErrTooLarge`.

## The flowchart subset

| Supported | |
|---|---|
| header | `flowchart` or `graph`, with `TB`/`TD`, `BT`, `LR` or `RL`; statements may follow on the header line after `;` |
| shapes | `[ ]` `( )` `([ ])` `[[ ]]` `[( )]` `(( ))` `> ]` `{ }` `{{ }}` `[/ /]` `[\ \]` `[/ \]` `[\ /]` `((( )))` |
| links | `--`, `-.-`, `==`, `~~~` of any length (a longer link sets `MinLen`); `>`, `o` and `x` ends; `<`, `o` and `x` at the start |
| link text | `A -- text --> B`, `A -. text .-> B`, `A == text ==> B`, and `A -->\|text\| B` |
| chains | `A --> B --> C`, `A & B --> C & D` (one edge per pair) |
| subgraphs | `subgraph id`, `subgraph id [title]`, `subgraph "title"`, nesting, `direction` inside |
| styling | `classDef` (including `default`), `class`, `:::`, `style`; a node's `Style` is resolved |
| text | quoted labels, Markdown strings (marks removed), entity codes (`#quot;`, `#9829;`), `<br>` |
| other | `%%` comments and `%%{ }%%` directives (ignored), `accTitle:` and `accDescr:` (ignored), `;` separators |

Outside the subset, so `ErrUnsupported`: `click`, `linkStyle`, Font Awesome icons (`fa:`), the
`@{ }` shape and edge-id syntax, front matter, `accDescr { }` blocks, a link to a subgraph, other
HTML in a label, and every other diagram type.

## The model

- `Node`: `ID`, plain `Label`, `Shape`, `Classes`, a resolved `Style`, and `Subgraph`, the last
  subgraph it is mentioned in. A mention at top level does not take a node out of its subgraph.
- `Edge`: `From`, `To`, `Label`, `Line`, `Head` (at `To`), `Tail` (at `From`), `MinLen`.
- `Subgraph`: `ID`, `Title`, its own `Dir` (zero: its parent's), `Parent`.
- Every node, edge and subgraph carries a `Span` of source bytes, for errors and for editing.
