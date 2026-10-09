package highlight

import "github.com/yongjohnlee80/golib/indent"

// Source pairs a document's semantic highlighter and optional indentation policy.
// A mutable source has one owner. A factory creates a fresh instance per document.
type Source struct {
	Highlighter Highlighter
	Indenter    indent.Policy
	States      StateStore
}

// StateStore manages leases on interned source states. Retain new roots before
// releasing replaced roots. Collect visits at most budget nodes and reports
// whether further reclamation work remains. State zero needs no lease.
type StateStore interface {
	Retain(State)
	Release(State)
	Collect(budget int) bool
}

// Overlay decorates semantic spans for painting. It must not mutate its input
// and returns ordered, disjoint, in-bounds byte spans. It receives no lexical state.
type Overlay func(line string, semantic []Span) []Span

// NewSource creates the definition's document-owned behavior using catalog for
// embedded language lookup. Legacy definitions supply highlighting only.
func (d Definition) NewSource(catalog *Catalog) Source {
	if d.SourceFactory != nil {
		return d.SourceFactory(catalog)
	}
	return Source{Highlighter: d.Highlighter}
}
