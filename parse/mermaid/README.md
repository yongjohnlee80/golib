# parse/mermaid

Parses Mermaid diagrams into models a native renderer draws: flowcharts (`flowchart` and
`graph`), sequence, class, state, ER, journey, mindmap, timeline, requirement and gantt diagrams. Every
other type is `ErrUnsupported`. Standard
library only.

```go
import "github.com/yongjohnlee80/golib/parse/mermaid"

d, err := mermaid.Parse(src)
switch {
case errors.Is(err, mermaid.ErrUnsupported), errors.Is(err, mermaid.ErrTooLarge):
	// hand it to another renderer
	var nd *mermaid.NotDrawnError // its Line (0: the type or the whole source) and What
case err != nil:
	var se *mermaid.SyntaxError // its Line and Col, to show in place
}
switch d := d.(type) { // d.Kind() says the same
case *mermaid.FlowchartDiagram: // Dir, Nodes, Edges, Subgraphs, Classes
case *mermaid.SequenceDiagram:  // Title, Participants, Steps
case *mermaid.ClassDiagram, *mermaid.StateDiagram, *mermaid.ERDiagram:
case *mermaid.JourneyDiagram, *mermaid.MindmapDiagram, *mermaid.TimelineDiagram, *mermaid.RequirementDiagram:
case *mermaid.GanttDiagram: // Title, AxisFormat, Sections of Tasks with resolved Start and End
}
```

## The answer is all or nothing

`Parse` reads the whole diagram before it answers. If one construct anywhere is outside the
supported subset, the whole diagram is `ErrUnsupported`, so a renderer never draws part of a
diagram as if it were all of it. `SyntaxError` is for a malformed construct inside the grammar
this parser claims. When a diagram has both, `ErrUnsupported` wins, because the other renderer
may read what this one cannot.

A source over 64 KiB (`MaxSource`), or a label over 1 KiB (`MaxLabel`), is `ErrTooLarge`.

Every `ErrUnsupported` and `ErrTooLarge` is a `*NotDrawnError`: `What` was refused and the `Line`
it is on, 0 for the diagram's type or the whole source, so a checker can point at it as it does
at a `SyntaxError`. Its text is the error's.

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
| `journey` | `title`; `section`s; tasks `Name: score: actor, actor` (score 1 to 5) | `accDescr { }` blocks, HTML in a name |
| `mindmap` | a tree by indentation; plain text or an id and its shape: `[ ]` `( )` `(( ))` `)) ((` `) (` `{{ }}`; Markdown strings, `<br>` | `::icon( )`, `:::class`, HTML in a label |
| `timeline` | `title`; `section`s; periods with events on their line or on `: event` lines under them | a direction, `accDescr { }` blocks, HTML in a label |
| `requirementDiagram` | the six requirement types (`id`, `text`, `risk`, `verifymethod`) and `element`s (`type`, `docref`); every relationship both ways; `direction`; styling | `click`, `linkStyle`, `title`, `accDescr { }` blocks |
| `erDiagram` | entities, quoted and aliased; attribute blocks (type, name, PK/FK/UK, comment); every cardinality symbol and its word forms; identifying `--` and not `..`; `direction`; styling | `click`, `linkStyle`, `title` and other statements it does not know |
| `gantt` | `title`; `dateFormat YYYY-MM-DD`; `axisFormat` with `%Y %y %m %d %e %b %B %a %A %j %%`; `todayMarker off`; `section`s; tasks `Name : [done,active,crit,milestone,] [id,] [start,] end`, where start is a date, `after id …` or the task before's end, and end is a date (exclusive) or a length in whole days or weeks (`3d`, `2w`); a milestone keeps its length and is drawn at its middle; a setting's value ends at `#` or `;`; spans up to 100 years | other date formats, times, other length units, other axis directives, `excludes`/`includes`/`weekday`/`weekend`, `inclusiveEndDates`, `tickInterval`, `topAxis`, `displayMode`, `click`/`call`, a today marker (none is drawn, so a chart reads the same every day), `until`, `after` a task defined later, a first task with no start, IDs other than letters, digits, `_` and `-`, `#` comment lines, a chart with no task |
| `gitGraph` | `LR:`, `TB:` or `BT:`; `commit` (`id`, `tag`, `type: NORMAL\|REVERSE\|HIGHLIGHT`); `branch` (`order`); `checkout`/`switch`; `merge` (`id`, `tag`, `type`); `cherry-pick id:` (`tag`); main is `main`; a misuse Mermaid refuses (an unknown branch, a merge into itself or of nothing new, an id twice) is a `SyntaxError` | `msg:`, a cherry-pick's `parent:` or of a merge commit, `accDescr { }` blocks |

## The model

- `Node`: `ID`, plain `Label`, `Shape`, `Classes`, a resolved `Style`, and `Subgraph`, the last
  subgraph it is mentioned in. A mention at top level does not take a node out of its subgraph.
- `Edge`: `From`, `To`, `Label`, `Line`, `Head` (at `To`), `Tail` (at `From`), `MinLen`.
- `Subgraph`: `ID`, `Title`, its own `Dir` (zero: its parent's), `Parent`.
- Every node, edge and subgraph carries a `Span` of source bytes, for errors and for editing.
