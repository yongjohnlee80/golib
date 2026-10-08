# code

Declaration-level chunking of source code for retrieval, as golib `search.Chunker`s.

## Install

```go
import "github.com/yongjohnlee80/golib/search/chunk/code"
```

## Features

| language | extensions | units |
|---|---|---|
| Go (`go/parser`) | `.go` | the file header (build tags, package doc, imports), functions and methods, each type spec, `const`/`var` groups |
| TypeScript / JavaScript | `.ts` `.tsx` `.js` `.jsx` `.mjs` | functions, classes and their methods, interfaces, type aliases, exported `const` arrow functions; JSDoc and decorators kept |
| Python | `.py` | `def`, `async def`, `class` and its methods, by indentation; decorators and docstrings kept |
| Rust | `.rs` | `fn`, `struct`, `enum`, `union`, `trait` and `impl` blocks with their functions; `///` docs and attributes kept |

- **One partition rule for every language:** spans ascend and never overlap, only whitespace lies
  outside them, and `Body == string(Doc.Text[ByteStart:ByteEnd])`. Detached comments, directives and
  top-level statements become header and glue chunks.
- **Two surfaces:** `Body` is the verbatim code; `Embed` is breadcrumb, doc and signature, never the
  function body. A Go `const` or `var` embeds its names, type and values, a composite literal as
  its type and first three elements (`[][3]int{{0x0, 0xf, 0}, …}`), a function literal as its
  signature.
- **Bounded embeddings:** no `Embed` is larger than the token budget its chunk's body is held to;
  a doc too long for it is cut, the signature kept whole.
- **Oversized declarations** split at their body's statements; every fragment's breadcrumb carries
  the signature, and later fragments embed the signature with their comments and the identifiers
  they declare and call. A fragment of a long type or table embeds its breadcrumb and its own first
  lines, never the whole declaration.
- **String-aware:** braces and keywords inside strings, template literals, raw strings, char
  literals and comments never start or end a unit.
- **A Go file that does not parse** falls back to golib's `chunk.Text`, so it stays searchable.
- **A version per language** (`code-go-2+text-5`, `code-ts-2`, `code-py-2`, `code-rs-2`).

## Example

```go
var chunkers search.Chunkers
if err := code.Register(&chunkers); err != nil {
	return err
}
c, _ := chunkers.For("server/handler.go")
chunks, err := c.Chunk(search.Doc{Path: "server/handler.go", Text: src})
```

## License

See [LICENSE](../../../LICENSE).
