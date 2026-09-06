// Package sql supplies SQL dialects to the lexer as lexical FORM VALUES,
// assembled from golib/parse's generic constructors.
//
// This is the only one of the three packages that names a dialect, and that is
// the whole arrangement: because the core cannot name one, it is closed to none
// of them. Everything here is data — character classes, literal sets, and an
// order — handed to a lexer that has never heard of SQL. Adding a dialect means
// adding a function here, not editing anything below.
//
// # Order is precedence
//
// The lists are returned in the order the lexer must try them, and the order
// carries real decisions: `/*` before `/`, `--` before `-`, `E'` before the word
// run that would otherwise eat the E. The core does not sort or guess, because
// which construct wins is a dialect's knowledge.
//
// # What these lists say, and what they do not
//
// A kind says what a run of bytes IS, never what it means. `SELECT` comes back
// as a Word here; that it is a verb in one statement and a column name in another
// is a judgement for the layer above, made with the verbatim bytes still in front
// of it. There are no keywords in this package for that reason.
//
// Every list ends with a one-byte fallback, so no byte is ever left unclaimed.
package sql
