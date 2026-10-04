# parse/view — the .view file, parsed and resolving nothing

A `.view` file declares how to materialize documents from a database. It has YAML frontmatter that
says what to query, and a body template that says what each row becomes:

```text
---
version: 1
name: track_view
source: postgres
args: [entity_ids]
process: |
  SELECT jsonb_build_object('entity_id', t.id, 'track', t.title)
  FROM track t
  WHERE t.id = ANY($1)
export: track_{{.entity_id}}.md
---
# {{.track}}
```

This package reads that shape and stops there. The frontmatter is a [`parse/yaml`](../yaml/README.md)
tree, with no field checked and no tag resolved. The body is a `text/template/parse` tree, with no
function name checked. What the fields mean, which functions exist, and how a view runs are
decided by [`golib/view`](../../view/README.md), the same way [`yaml`](../../yaml/README.md)
evaluates what `parse/yaml` reads.

## Install

```sh
go get github.com/yongjohnlee80/golib
```

```go
import pview "github.com/yongjohnlee80/golib/parse/view"
```

## Parsing

```go
f, err := pview.Parse(src, pview.WithName("track.view"))
if err != nil {
    var pe *pview.Error // where, and what is wrong there
    errors.As(err, &pe)
    return err
}

f.Meta                            // the frontmatter's *yaml.Document, nil when it is empty
f.Templates[pview.BodyTemplate]   // the body's template tree; each {{define}} sits beside it
f.Source[f.Body.Start:f.Body.End] // the body as written
```

| field | what it is |
| --- | --- |
| `Open`, `Close` | the two delimiter lines, each exactly `---` (a trailing `\r` is tolerated) |
| `Front` | the frontmatter between them |
| `Body` | everything after the closing line, to the end of the file |
| `Meta` | the frontmatter as a YAML document; its spans index the frontmatter |
| `Templates` | the body's template trees, under `BodyTemplate` and each defined name |

The body may begin with `---` of its own, a Markdown document's frontmatter. Only the first pair
of delimiter lines belongs to the view.

## Positions are the file's

Every position counts from the top of the `.view` file, never from the start of a part:

- `File.Position(off)` gives the line and column of a byte offset. Columns count characters.
- `File.MetaPosition(off)` translates a YAML node's span, which indexes the frontmatter.
- `File.TemplatePosition(pos)` translates a template node's `Pos`. The template parser places an
  action at its first token, so `{{.title}}` is where `.title` begins.
- A YAML error is reported at its own line in the file.
- A template error is placed at the body's first line. Its message, from the template parser,
  names the file's line where parsing stopped: `template: body:12: unexpected EOF` is line 12 of
  the file.

## What is an error

Each is an `*Error` with a position; `errors.Unwrap` gives the YAML or template error behind it.

- The first line is not exactly `---`.
- No later line is exactly `---`. An indented `---` is not a delimiter.
- The frontmatter is not well-formed YAML, or holds more than one YAML document.
- The body is not a well-formed template. An undefined function is **not** an error here.

`MaxDepth(n)` bounds the frontmatter's nesting, as `parse/yaml`'s option of the same name does.

## License

See [LICENSE](../../LICENSE).
