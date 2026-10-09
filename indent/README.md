# indent

Install with `go get github.com/yongjohnlee80/golib/indent`.

`Policy` is a UI-free seam for source-language indentation. A request carries the
current line, byte cursor, editing operation and verified incoming source state.
A decision supplies whitespace, optionally separating a preserved matching closer.
It never owns cursor movement, text mutation, paste or undo.

```go
var policy indent.Policy = indent.PolicyFunc(func(r indent.Request) (indent.Decision, bool) {
    return indent.Decision{Prefix: indent.Leading(r.Line)}, r.StateKnown
})
```

Source providers register a policy beside their highlighter. Editors may opt in,
override the indent unit, and conservatively retain whitespace when context is
not known. See the [license](../LICENSE).
