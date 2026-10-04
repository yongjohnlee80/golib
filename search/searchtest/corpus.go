package searchtest

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/search/vector"
)

// Doc is one document of the conformance corpus, for a fixture to index through its own write path.
type Doc struct {
	Path   string
	Tags   []string            // lowercased
	Facets map[string][]string // field → values
	Chunks []string            // each chunk's body, in order: chunk i has ordinal i
	Links  []string            // the paths this document links to (a repeat and a link to itself count no more)
	// Unready marks a document whose vectors are not ready: a Semantic store does not answer from it,
	// and its SemanticState is StatePartial while it is in the store.
	Unready bool
}

// Dims is the size of Embed's vectors.
const Dims = 64

// Embed is the deterministic embedding of the corpus: each word's count, hashed into Dims buckets,
// normalized. A fixture stores Embed(body) as each chunk's vector, and the suite queries with
// Embed(words), so texts that share words are near.
func Embed(text string) []float32 {
	v := make([]float32, Dims)
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%Dims]++
	}
	return vector.Normalize(v)
}

// Corpus is the documents every conformance check runs over. Its words are made up, so no stemmer
// or stop list turns one into another.
func Corpus() []Doc {
	docs := []Doc{
		{Path: "keep/k1.md", Tags: []string{"keep", "shared"}, Facets: map[string][]string{"kind": {"keep"}, "level": {"one"}},
			Chunks: []string{"zorblax filler words here", "second chunk quindle"}},
		{Path: "keep/k2.md", Tags: []string{"keep"}, Facets: map[string][]string{"kind": {"keep"}, "level": {"two"}},
			Chunks: []string{"zorblax more filler words"}},
		{Path: "keep/k3.md", Tags: []string{"keep", "shared"}, Facets: map[string][]string{"kind": {"keep"}},
			Chunks: []string{"zorblax yet more filler"}},
		// a sibling directory whose name starts like keep/: a path filter on keep must not admit it
		{Path: "keeper/k4.md", Tags: []string{"keep"}, Facets: map[string][]string{"kind": {"keep"}},
			Chunks: []string{"zorblax boundary filler words"}},
		{Path: "tie/a.md", Chunks: []string{"plinth"}},
		{Path: "tie/b.md", Chunks: []string{"plinth"}},
		{Path: "tie/c.md", Chunks: []string{"plinth"}},
		{Path: "links/hub.md", Chunks: []string{"hub text vorpal"}},
		{Path: "links/s1.md", Chunks: []string{"one vorpal source"}, Links: []string{"links/hub.md"}},
		{Path: "links/s2.md", Chunks: []string{"two vorpal source"}, Links: []string{"links/hub.md", "links/hub.md", "links/s2.md"}},
		{Path: "snap/x.md", Chunks: []string{"delta snapshot text"}},
		{Path: "late/unready.md", Chunks: []string{"glimmer late text"}, Unready: true},
	}
	// noise outranks keep/ for zorblax by words and by vector, so a filter applied after the limit
	// leaves nothing of keep/
	for i := range 30 {
		docs = append(docs, Doc{Path: fmt.Sprintf("noise/n%02d.md", i), Tags: []string{"noise"},
			Facets: map[string][]string{"kind": {"noise"}}, Chunks: []string{"zorblax zorblax zorblax noise"}})
	}
	return docs
}
