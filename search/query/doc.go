// Package query reads a search query: its words as terms to match literally, and its name:value
// words as filters on declared fields. It renders terms for SQLite FTS5. It imports no other search
// package, so any of them can use it.
//
//	terms, words := query.Terms(`storage "quoted" stor*`)
//	match := query.FTS5(terms) // "storage" """quoted""" "stor"*
//	rest, facets, err := query.Facets("type:adr storage", nil, schema)
package query
