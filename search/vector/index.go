package vector

import (
	"cmp"
	"errors"
	"iter"
	"slices"
)

// Code is one chunk's 1-bit code. C is the store's chunk key.
type Code[C any] struct {
	Chunk C
	Bits  []uint64
}

// Index is the codes of one model at one watermark, by document, never changed once made: the next
// commit makes another with Next, sharing the documents that did not change. A store publishes the
// current one (in an atomic.Pointer, say) and a query uses it only when it is the query's own: same
// model, same watermark (Usable). D is the store's document key, C its chunk key.
type Index[D comparable, C any] struct {
	model     string
	watermark int64
	docs      map[D][]Code[C]
}

// NewIndex is the index of model's codes at watermark: the store's number for the committed state
// the codes were read from (a commit sequence, say). The index keeps codes; the caller does not
// change it after.
func NewIndex[D comparable, C any](model string, watermark int64, codes map[D][]Code[C]) *Index[D, C] {
	if codes == nil {
		codes = map[D][]Code[C]{}
	}
	return &Index[D, C]{model: model, watermark: watermark, docs: codes}
}

// Model is the model the codes are of.
func (x *Index[D, C]) Model() string { return x.model }

// Watermark is the committed state the codes reflect.
func (x *Index[D, C]) Watermark() int64 { return x.watermark }

// Next is the index at watermark after a commit: the changed documents' codes are dropped and
// fresh's added (a changed document with no codes now is simply gone); every other document's are
// shared. With nothing changed, the codes are all shared and only the watermark moves, so a commit
// that changes no code still publishes cheaply. fresh and watermark must come from one committed
// read snapshot.
func (x *Index[D, C]) Next(watermark int64, changed []D, fresh map[D][]Code[C]) *Index[D, C] {
	if len(changed) == 0 {
		return &Index[D, C]{model: x.model, watermark: watermark, docs: x.docs}
	}
	docs := make(map[D][]Code[C], len(x.docs)+len(fresh))
	for d, cs := range x.docs {
		docs[d] = cs
	}
	for _, d := range changed {
		delete(docs, d)
	}
	for d, cs := range fresh {
		docs[d] = cs
	}
	return &Index[D, C]{model: x.model, watermark: watermark, docs: docs}
}

// Usable reports whether the index may answer a query whose snapshot read model and watermark. An
// older index lacks chunks that became ready since; a newer one lacks chunks deleted since but
// still alive in the query's snapshot; another model's codes are not comparable. Either way it
// could answer falsely empty, and the query scans the stored codes instead. A nil index is never
// usable.
func (x *Index[D, C]) Usable(model string, watermark int64) bool {
	return x != nil && x.model == model && x.watermark == watermark
}

// Codes is every code in the index, in no set order.
func (x *Index[D, C]) Codes() iter.Seq[Code[C]] {
	return func(yield func(Code[C]) bool) {
		for _, cs := range x.docs {
			for _, c := range cs {
				if !yield(c) {
					return
				}
			}
		}
	}
}

// Nearest is the chunks of codes ordered by Hamming distance to q, nearest first; equal distances
// are ordered by cmp on the chunk keys, so the order is the same however codes are iterated.
func Nearest[C any](codes iter.Seq[Code[C]], q []uint64, cmpChunk func(a, b C) int) []C {
	type scored struct {
		chunk C
		dist  int
	}
	var all []scored
	for c := range codes {
		all = append(all, scored{c.Chunk, Hamming(c.Bits, q)})
	}
	slices.SortFunc(all, func(a, b scored) int {
		if a.dist != b.dist {
			return cmp.Compare(a.dist, b.dist)
		}
		return cmpChunk(a.chunk, b.chunk)
	})
	out := make([]C, len(all))
	for i, s := range all {
		out[i] = s.chunk
	}
	return out
}

// Vec is an item with its float vector, as a TwoStage fetch returns it; Dot is filled by TwoStage.
type Vec[T any] struct {
	Item T
	F32  []float32
	Dot  float64
}

// SkipRest, returned by a TwoStage fetch, ends the scan without error: no later window can hold a
// valid item (the filters admit no document at all, say).
var SkipRest = errors.New("vector: skip the rest")

// TwoStage rescores a Hamming order by the float vectors. It hands fetch the order window keys at a
// time; fetch returns the items still valid in the caller's snapshot (alive, ready, admitted by its
// filters), with their vectors. Their dot products with q rank them, equal ones ordered by less;
// windows are taken until n items survive or the order runs out, so stale codes ahead of valid ones
// cannot crowd them out. The result is the best n.
func TwoStage[C, T any](order []C, q []float32, window, n int, fetch func(ids []C) ([]Vec[T], error), less func(a, b T) bool) ([]Vec[T], error) {
	window = max(window, 1)
	var valid []Vec[T]
	for start := 0; start < len(order) && len(valid) < n; start += window {
		got, err := fetch(order[start:min(start+window, len(order))])
		if errors.Is(err, SkipRest) {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, v := range got {
			v.Dot = Dot(q, v.F32)
			valid = append(valid, v)
		}
	}
	slices.SortFunc(valid, func(a, b Vec[T]) int {
		if a.Dot != b.Dot {
			return cmp.Compare(b.Dot, a.Dot)
		}
		switch {
		case less(a.Item, b.Item):
			return -1
		case less(b.Item, a.Item):
			return 1
		}
		return 0
	})
	return valid[:min(len(valid), max(n, 0))], nil
}
