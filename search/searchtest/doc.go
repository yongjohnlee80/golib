// Package searchtest checks a search.Store against the port's contract, so a consumer that writes
// its own tables can show its store answers as the Engine expects: Run indexes a fixed corpus
// through the consumer's write path and checks lexical and semantic retrieval, the filter
// semantics, tie order, one snapshot per View, and the optional capabilities.
//
//	func TestMyStore(t *testing.T) {
//		searchtest.Run(t, func(t testing.TB, docs []searchtest.Doc) searchtest.Fixture[int64, *myView] {
//			s := openMyStore(t)
//			for _, d := range docs {
//				s.write(d) // storing searchtest.Embed(body) as each chunk's vector
//			}
//			return searchtest.Fixture[int64, *myView]{Store: s, Model: s.model, Change: s.rewrite}
//		})
//	}
//
// MemStore is a store held in memory that passes the suite: a reference implementation, and a
// store for tests of code that searches.
package searchtest
