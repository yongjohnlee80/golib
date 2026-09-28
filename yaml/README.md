# yaml — evaluate a YAML tree under a schema

`yaml` takes a document parsed by [`parse/yaml`](../parse/yaml/README.md), resolves each node's tag
through one of YAML 1.2's [recommended schemas](https://yaml.org/spec/1.2.2/#chapter-10-recommended-schemas),
and constructs Go values. The parser builds the tree and resolves nothing; this package decides what
it means.

```go
st, err := pyaml.Parse(frontmatter)
v, err := yaml.Evaluate(st.Docs[0], yaml.Core)
title, _ := v.(yaml.Map).Get("title")
```

## Schemas

| Schema | untagged plain scalar | an explicit or `!` tag |
| --- | --- | --- |
| `Failsafe` | left unresolved (so are untagged collections) | `!` resolves by kind: seq, map, str |
| `JSON` | `null`; `true`/`false`; JSON integers; JSON floats. Anything else is an **error** | as Failsafe |
| `Core` | `null`, `Null`, `NULL`, `~`, empty; `true`/`True`/`TRUE`/`false`/…; decimal, `0o` octal and `0x` hex integers; floats, `.inf`, `.nan`. Anything else is a **string** | as Failsafe |

`Resolve(doc, schema)` is the resolution step alone, for any schema: it maps each node to its tag,
leaving out what the schema leaves unresolved. `Evaluate(doc, schema)` resolves, then constructs
`nil`, `bool`, `int64`, `float64`, `string`, `[]any` and `Map`. It takes `JSON` or `Core`; `Failsafe`
defines no complete value, so `Evaluate` refuses it with `ErrFailsafe`.

`Map` is a mapping as written: its entries in source order, with keys of any kind (a sequence, a
number), which a Go map would lose. `Get(key)` finds a string key, the frontmatter case.

## What is an error

Each is an `*Error` with a position.

- **An explicit tag outside the schema** (`!!binary`, `!local`), or content its tag cannot hold
  (`!!int abc`). A caller that knows more tags wraps `Evaluate`.
- **An integer beyond `int64`**, in any base. It is never silently a float.
- **Two equal keys in one mapping**, by the spec's node equality after resolution: the same tag and
  canonical form, so `1` and `0x1` are one key and `1` and `1.0` are two; a sequence compares in order,
  and a mapping as a set, so `{a: 1, b: 2}` and `{b: 2, a: 1}` are one key. The error names both keys.
- **An alias cycle**: an alias inside the node it names. A Go value cannot hold it; the parser's tree
  can, since it keeps aliases unexpanded.
- **Too many nodes.** An alias constructs its target again each time it is used, and every
  construction counts toward `MaxNodes(n)` (default 10⁶). A document whose aliases double at each level
  (the "billion laughs" shape) stops there, before it is built.

## Conformance

Every valid yaml-test-suite test with an `in.json` evaluates, document by document under `Core`, to
that JSON: 279 of 279. The suite constructs tags it does not define (`!foo`, `!!set`) by node kind, so
the test gives those nodes the non-specific `!` first, as a caller that knows more tags would.
Chapter 10's tables have their own cases, for each schema.
