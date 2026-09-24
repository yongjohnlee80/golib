// Package qml parses QML.
//
// It is faithful to QML and has no extensions of its own. It briefly had one, a
// `@name` sigil for a style token, and it is gone: QML defines styles with a
// singleton object reached through an import, so the sigil was a second way to
// spell something the language already had.
//
// # What this package does NOT do
//
// It does not judge meaning. Whether a type exists, whether a property can hold
// a colour, whether a handler names something reachable — none of that is
// answerable from syntax, and a parser that guessed would refuse valid
// documents to enforce a rule it cannot evaluate. The tree records what was
// WRITTEN; a consumer decides what it means.
//
// That separation is what lets the grammar stay complete while an evaluator
// stays small. Everything this package reads and a consumer cannot run is
// refused BY THE CONSUMER, by name and with a position — which reads as "not
// supported yet" rather than "your document is wrong", and only one of those is
// true.
//
// JavaScript inside a document — property values and handler bodies — is parsed
// by [github.com/yongjohnlee80/golib/parse/js], on this parser's own scanner
// and depth budget.
package qml
