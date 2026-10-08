# parse/mermaid

Parses Mermaid diagrams into models a native renderer draws: flowcharts (`flowchart` and
`graph`), sequence, class, state and ER diagrams. Every other type is `ErrUnsupported`. Standard
library only.

```go
import "github.com/yongjohnlee80/golib/parse/mermaid"

d, err := mermaid.Parse(src)
switch {
case errors.Is(err, mermaid.ErrUnsupported), errors.Is(err, mermaid.ErrTooLarge):
	// hand it to another renderer
case err != nil:
	var se *mermaid.SyntaxError // its Line and Col, to show in place
}
switch d := d.(type) { // d.Kind() says the same
case *mermaid.FlowchartDiagram: // Dir, Nodes, Edges, Subgraphs, Classes
case *mermaid.SequenceDiagram:  // Title, Participants, Steps
case *mermaid.ClassDiagram, *mermaid.StateDiagram, *mermaid.ERDiagram:
}
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

## The other types

Each has its own model and the same answer: all of it, or `ErrUnsupported`.

| Type | Supported | `ErrUnsupported` |
|---|---|---|
| `sequenceDiagram` | `participant`/`actor` with `as`; every arrow (`->`, `-->`, `->>`, `-->>`, `-x`, `--x`, `-)`, `--)`, `<<->>`, `<<-->>`) with `+`/`-` activation; notes left of, right of, over one or two; `activate`/`deactivate`; `loop`, `alt`/`else`, `opt`, `par`/`and`, `critical`/`option`, `break`, `rect`; `autonumber`; `title` | `box`, `create`, `destroy`, links and menus, participant types (`@{ }`) |
| `classDiagram` | classes with members (`{ }` or `Name : member`), generics `~T~`, labels, annotations `<<x>>`; every relation, both ways, with cardinalities and a label; `namespace`; `note`/`note for`; `direction`; styling | `click`, `callback`, `link`, lollipop interfaces, nested namespaces |
| `stateDiagram`, `-v2` | states with descriptions; `[*]` per scope; transitions with labels; composites, nested; `<<fork>>`, `<<join>>`, `<<choice>>`; notes, one-line or to `end note`; `direction`; styling | concurrent regions (`--`), `hide empty description`, `click`, floating notes, a state across composites |
| `erDiagram` | entities, quoted and aliased; attribute blocks (type, name, PK/FK/UK, comment); every cardinality symbol and its word forms; identifying `--` and not `..`; `direction`; styling | `click`, `linkStyle`, `title` and other statements it does not know |

## The model

- `Node`: `ID`, plain `Label`, `Shape`, `Classes`, a resolved `Style`, and `Subgraph`, the last
  subgraph it is mentioned in. A mention at top level does not take a node out of its subgraph.
- `Edge`: `From`, `To`, `Label`, `Line`, `Head` (at `To`), `Tail` (at `From`), `MinLen`.
- `Subgraph`: `ID`, `Title`, its own `Dir` (zero: its parent's), `Parent`.
- Every node, edge and subgraph carries a `Span` of source bytes, for errors and for editing.
