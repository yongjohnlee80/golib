# parse/rust

Install: `go get github.com/yongjohnlee80/golib/parse/rust`.

`rust.Definition().NewSource(nil)` supplies tolerant Rust highlighting and
four-space structural indentation. It distinguishes lifetime tokens from
character literals, carries nested comments, and retains arbitrary raw-string
hash delimiters. It does not perform type checking or format a file on load.

Register `Definition()` in the shared catalog; source files use `*.rs` and
Markdown uses `rust`/`rs`. Keep the complete source's state store with the editor.
See [languages](../languages/README.md) and [LICENSE](../../LICENSE).
