# parse/typescript

Install: `go get github.com/yongjohnlee80/golib/parse/typescript`.

`typescript.Definition().NewSource(nil)` supplies TypeScript vocabulary and
types over shared JavaScript lexical mechanics: comments, templates, expression
regexp recognition and embedded JSX. Default structural indentation is two
spaces. This does not broaden the strict JavaScript expression evaluator.

Register it for `*.ts`/`*.tsx` and `typescript`/`ts`/`tsx` fences. See
[languages](../languages/README.md) and [LICENSE](../../LICENSE).
