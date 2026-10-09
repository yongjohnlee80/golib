# lexical

Internal tolerant scanner mechanics selected by source-language declarations.
Functional options choose vocabulary, literal delimiters, comments and indent
families. Sessions own immutable interned lexical/structural context; policies
analyze a current line in scratch context without creating persistent states.

Used by source providers; this is not a public parser framework. See
[languages](../../languages/README.md) and [LICENSE](../../../LICENSE).
