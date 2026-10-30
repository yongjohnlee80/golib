// Package query parses user search strings into structured lexical search terms and
// metadata facet filters, producing dialect-compliant query expressions for SQLite FTS5.
//
// # Syntax and Parsing Rules
//
//   - Lexical Terms: Words separated by whitespace are extracted by [Terms], recognizing
//     a trailing prefix wildcard ('*') on the final word only. Quotes are preserved as
//     literal characters rather than grouping phrases.
//   - Facet Filtering: Qualified key-value pairs (`name:value`) targeting declared schema fields
//     are extracted into typed filters via [Facets], leaving unconsumed text for free-text search.
//   - SQLite FTS5 Synthesis: [FTS5] transforms parsed terms into safe MATCH syntax by producing
//     one quoted FTS5 token per term, with inner quotes doubled. [FTS5Any] joins the same tokens with OR, so
//     any one word matches: the fallback for a query whose every word no row holds.
//
// The package imports no other search modules and has zero external dependencies.
package query
