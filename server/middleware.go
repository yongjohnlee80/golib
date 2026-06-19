package server

// Middleware wraps a handler with cross-cutting behavior. It is generic over H so
// the same model serves HTTP (H = http.Handler), WebSocket, etc.
type Middleware[H any] func(next H) H

// Chain is an ordered, IMMUTABLE middleware builder. Use/When/Extend each return a
// NEW Chain over a freshly copied middleware slice — they never mutate the
// receiver — so deriving per-group chains from a shared base is safe:
//
//	base := NewChain(a, b)
//	g1 := base.Use(c) // a, b, c
//	g2 := base.Use(d) // a, b, d  (base, g1, g2 are disjoint)
type Chain[H any] struct {
	mws []Middleware[H]
}

// NewChain builds a chain from the given middleware (copied).
func NewChain[H any](mws ...Middleware[H]) *Chain[H] {
	return &Chain[H]{mws: append([]Middleware[H](nil), mws...)}
}

// Use returns a new Chain = receiver's middleware followed by mws.
func (c *Chain[H]) Use(mws ...Middleware[H]) *Chain[H] {
	next := make([]Middleware[H], 0, len(c.mws)+len(mws))
	next = append(next, c.mws...)
	next = append(next, mws...)
	return &Chain[H]{mws: next}
}

// When returns Use(mws...) when cond is true, else the receiver unchanged (safe to
// return because the chain is immutable).
func (c *Chain[H]) When(cond bool, mws ...Middleware[H]) *Chain[H] {
	if !cond {
		return c
	}
	return c.Use(mws...)
}

// Extend returns a new Chain = receiver's middleware followed by other's.
func (c *Chain[H]) Extend(other *Chain[H]) *Chain[H] {
	if other == nil {
		return c
	}
	return c.Use(other.mws...)
}

// Then applies the chain to a final handler. The FIRST middleware added (Use'd) is
// the OUTERMOST — it wraps all the others.
func (c *Chain[H]) Then(h H) H {
	for i := len(c.mws) - 1; i >= 0; i-- {
		h = c.mws[i](h)
	}
	return h
}

// Len reports the number of middleware in the chain.
func (c *Chain[H]) Len() int { return len(c.mws) }
