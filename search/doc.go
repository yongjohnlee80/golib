// Package search runs hybrid search over a store the application owns: lexical and semantic
// retrieval, reciprocal rank fusion, link and tag boosts, and a cap per document.
//
// The application keeps its tables, migrations and SQL, and implements Store: one consistent
// snapshot per View, with Lexical on every view and Semantic, Signaler and Lister as capabilities.
// The Engine reads only through the View it is given:
//
//	eng := search.NewEngine[int64, *myView](store, search.WithQueryEmbedder(embedQuery))
//	res, err := eng.Search(ctx, search.Query{Text: "type:adr storage", Fields: schema})
//
// Searcher is the interface any engine satisfies, so the one in use can be swapped. Chunkers is a
// registry of Chunker by file extension, which an application builds itself. The subpackages hold
// the parts: chunk (chunkers), embed (providers), vector (vector math and the code index), query
// (query parsing) and searchtest (a conformance suite for stores).
package search
