# parse/yaml — a YAML 1.2 parser that resolves nothing

`yaml` parses [YAML 1.2.2](https://yaml.org/spec/1.2.2/) into the specification's event stream and a
tree of nodes that point back into the source by byte span. It stops at the tree: a plain scalar is
its text and its style, and a tag is kept as written with its `%TAG` handle expanded. What `yes` or
`0x1F` means is a schema's to decide, and [`golib/yaml`](../../yaml/README.md) decides it, as
[`decl`](../../decl/README.md) evaluates what [`parse/qml`](../qml/README.md) parses.

```go
import pyaml "github.com/yongjohnlee80/golib/parse/yaml"

st, err := pyaml.Parse(src)                 // an ill-formed stream is an *Error with a position
root := st.Docs[0].Root                     // *Node: scalar, sequence, mapping or alias
pos := st.Position(root.Span.Start)         // line and column, on demand

for ev, err := range pyaml.Events(src) { … } // the same parse, as events, with no tree
```

## The tree

A `Stream` holds its `Docs`, the UTF-8 `Source` every span indexes, and the input's `Encoding`.
UTF-16 and UTF-32 input (known by a byte order mark or by the null bytes of its first character) is
transcoded first, so lines and columns are the same whatever the encoding was.

A `Document` has its `Directives` (`%YAML`, `%TAG`), its `Root`, and whether it began with `---` or
ended with `...`. A `Node` has:

| field | holds |
| --- | --- |
| `Kind` | `KindScalar`, `KindSequence`, `KindMapping`, `KindAlias` |
| `Style` | a scalar's plain, single-quoted, double-quoted, literal or folded form; a collection's block or flow form |
| `Tag` | as written, handle expanded; `"!"` for the non-specific tag; `""` when untagged |
| `Anchor` | the node's anchor name |
| `Value` | a scalar's content, after its style's line folding, escapes and chomping |
| `Items`, `Pairs` | a sequence's nodes; a mapping's pairs in source order, keys of any kind |
| `Alias`, `Target` | an alias's anchor name, and the node it names |
| `Span` | the node's bytes in `Source`, its properties included |

**Aliases are bound, not expanded.** `Target` is the most recent node before the alias, in the same
document, with that anchor (a later anchor of the same name shadows an earlier one). An alias to an
anchor that does not occur before it is an `*Error`. Duplicate mapping keys are kept: whether two
keys are equal depends on their resolved tags, so it is the evaluator's to report.

## Bounds

`MaxDepth(n)` (default 1000) bounds how deeply collections nest, in `Parse` and `Events` alike. Deeper
input is an `*Error`; a million levels is refused as quickly as a thousand and one.

## Conformance

The [yaml-test-suite](https://github.com/yaml/yaml-test-suite), data tag `data-2022-01-17`, is vendored
unmodified in `testdata/yaml-test-suite/` under its own MIT licence. **Every one of its 402 tests
passes:** each valid input's events equal its `test.event`, and each input marked `error` fails to
parse. `skip_test.go` is where a test the parser could not yet pass would be listed, and
`TestNoSkips` fails whenever it is not empty. (The suite's `in.json`, the constructed value, is
checked by `golib/yaml`.)

Where the parser departs from libyaml's structure it follows 1.2: `:` inside a plain scalar in flow
context, anchor names that hold `:`, tabs as separation but never as indentation, flow and quoted
lines indented past the enclosing block, implicit keys of a flow mapping that span lines, and the
rule that a block scalar's leading empty lines hold no more spaces than its first line.

`FuzzParse` checks that no input panics, spans nest, and the composed tree says exactly what the
events say. `TestParserResolvesNothing` pins the split: this package imports no schema code.

## As a `parse.Parser`

`yaml.New(opts...)` returns a `YAML`, a parser value that satisfies golib/parse's `Parser[*Stream]`
and `Named`, for code that holds parsers of several formats behind those interfaces:

```go
var p parse.Parser[*Stream] = yaml.New(yaml.MaxDepth(64))
```

A syntax error also answers as golib/parse's shared `parse.SyntaxError`, with the format and the
position; use a `parse.SyntaxError` value as the `errors.As` target. Its `*Error` and message stay.

**Unfinished or wrong.** A stream that ended in the middle of a construct is `Error.Incomplete`, and
answers `parse.ErrUnterminated`: more text appended could make it valid, so an editor or a prompt
can wait for it. Otherwise the error answers `parse.ErrSyntax`. Never both. Incomplete is reported
only where the parser can tell the input ran out:
- the grammar wanted more and got the end of the stream (`b: [2`, `a: {x: 1`);
- the scanner ran out inside a construct (`'quoted`, a cut escape);
- a character was cut off part way through its UTF-8 encoding.

So it is certain where it is reported, but it does not catch every truncation. A stream cut where
its last characters read as a different construct (a `-` of what would have been `---`, an alias
name cut short) is reported as wrong, one keystroke early, rather than leaving a caller waiting on a
real error. Over every failing prefix of the yaml-test-suite's valid streams it catches 95.5%.
