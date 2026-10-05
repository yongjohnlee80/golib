// Package rank is search's second stage: a Ranker, which scores texts against a query (a
// cross-encoder reading each pair together), its HTTP clients, and NewSearcher, a search.Searcher
// that re-ranks another Searcher's top candidates.
//
//	r, err := rank.NewTEI(ctx, "http://tei:8080", "", nil)
//	var h rank.Holder
//	h.Set(r, 40)
//	s := rank.NewSearcher(engine, texts, &h)
//	res, err := s.Search(ctx, search.Query{Text: "storage decision"})
//
// The ranker only sorts: the caller prepares the texts it reads (a TextSource), in any format.
// With no ranker in use the Searcher passes its inner Searcher's answer through; a ranker that
// fails leaves the hits in recall order, saying so in Result.Rank, unless the Searcher was built
// Required, when the search fails instead.
package rank
