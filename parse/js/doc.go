// Package js parses the C-family expression and statement grammar that other
// formats embed.
//
// It is a FORMAT IN ITS OWN RIGHT, not a helper for one consumer. QML is its
// first caller because a QML property value is a JavaScript expression and a
// QML signal handler is JavaScript statements, but nothing here knows that: the
// grammar is faithful to the language, and a consumer that cannot evaluate
// something the language contains refuses it by name and position rather than
// asking this package to stop reading it.
//
// # Dialects are data
//
// [JavaScript], [C] and [Go] are entries in a table of operator descriptors and
// feature flags, not branches. Adding a dialect is an entry; it is not a change
// to any function here, which is the test of whether an extension point is in
// the right place.
//
// # Embedding
//
// A format that embeds this one uses [Driver], which parses over a scanner the
// CALLER owns and shares the caller's depth budget. Both halves matter: the
// cursor has to come back where the embedded grammar left it, and a budget per
// grammar would bound the outer document's nesting while leaving the
// JavaScript inside it unbounded, in the same parse of the same file.
package js
