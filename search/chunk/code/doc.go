// Package code cuts source code into search chunks by declaration, as search.Chunkers:
// Go (parsed with go/parser), TypeScript and JavaScript, Python and Rust (hand-written extractors
// over one scanner that knows each language's strings and comments).
//
// Every chunker keeps one partition rule: chunk spans are ascending and never overlap, only
// whitespace lies outside them, and each chunk's Body is exactly its span of the source, so lexical
// search and an editor's jump see the file as it is. A declaration's doc comment belongs to it;
// anything else between declarations (a detached comment, a directive, a top-level statement)
// becomes a header chunk before the first declaration or a glue chunk after it.
//
// Each chunk has two surfaces: Body, the verbatim code lexical search indexes, and Embed, the
// breadcrumb, doc and signature an embedding is made of, without the function body, so control flow
// and error boilerplate do not pull every function's vector toward every other's. A declaration
// over the token budget splits at its body's statements; every fragment keeps the signature in its
// breadcrumb, and a later fragment embeds the signature with its own comments and the identifiers it
// declares and calls. A Go const or var embeds its values with a literal reduced to its type and first
// elements, and a fragment of a long type or table embeds its own first lines, not the declaration.
// No Embed is larger than the budget its body is held to: a doc too long for it is cut, never the
// signature.
//
// Each language has its own Version, so a change to one re-chunks only its files. Register adds
// every chunker to a search.Chunkers the caller builds; there is no global registry.
package code
