// Package query parses user search strings into structured lexical search terms and
// metadata facet filters, producing dialect-compliant query expressions for SQLite FTS5.
//
// # Syntax and Parsing Rules
//
//   - Lexical Terms: Bare words, quoted phrases (`"quoted phrase"`), and prefix wildcards (`term*`)
//     are extracted by [Terms].
//   - Facet Filtering: Qualified key-value pairs (`name:value`) targeting declared schema fields
//     are extracted into typed filters via [Facets], leaving unconsumed text for free-text search.
//   - SQLite FTS5 Synthesis: [FTS5] transforms parsed terms into safe, injection-proof MATCH syntax,
//     handling inner quote escaping and phrase group semantics.
//
// The package imports no other search modules and has zero external dependencies.
package query
