# parse/lua

Install: `go get github.com/yongjohnlee80/golib/parse/lua`.

`lua.Definition().NewSource(nil)` supplies Lua vocabulary, quoted and long-bracket
literals/comments, and keyword/delimiter indentation. `then`, `do`, `function`
and `repeat` use their matching closing/continuation rules; the default unit is
two spaces.

The same definition serves `*.lua` files and `lua` fences. It is tolerant lexical
editing behavior, independent of an evaluator and any UI. See
[languages](../languages/README.md) and [LICENSE](../../LICENSE).
