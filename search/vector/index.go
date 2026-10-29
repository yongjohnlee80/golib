package vector

import (
	"cmp"
	"errors"
	"hash/maphash"
	"iter"
	"maps"
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
	// The documents, in shards by their key's hash, so a commit copies only the shards its documents
	// are in: an index never changes, and a Next that copied one map of every document cost the
	// whole index for a one-document save. Next keeps seed, so a document stays in its shard.
	seed   maphash.Seed
	shards [shardCount]map[D][]Code[C]
}

// shardCount is how many shards an Index's documents are spread over.
const shardCount = 256

func (x *Index[D, C]) shardOf(d D) int { return int(maphash.Comparable(x.seed, d) % shardCount) }

// NewIndex is the index of model's codes at watermark: the store's number for the committed state
// the codes were read from (a commit sequence, say). The index keeps codes; the caller does not
// change it after.
func NewIndex[D comparable, C any](model string, watermark int64, codes map[D][]Code[C]) *Index[D, C] {
	x := &Index[D, C]{model: model, watermark: watermark, seed: maphash.MakeSeed()}
	for d, cs := range codes {
		i := x.shardOf(d)
		if x.shards[i] == nil {
			x.shards[i] = map[D][]Code[C]{}
		}
		x.shards[i][d] = cs
	}
	return x
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
	next := &Index[D, C]{model: x.model, watermark: watermark, seed: x.seed, shards: x.shards}
	if len(changed) == 0 && len(fresh) == 0 {
		return next
	}
	// Each shard a changed or fresh document is in is copied once, before its first change: x's
	// shards are shared with the indexes before and after it, and never written.
	var copied [shardCount]bool
	shard := func(d D) map[D][]Code[C] {
		i := next.shardOf(d)
		if !copied[i] {
			copied[i] = true
			if next.shards[i] == nil {
				next.shards[i] = map[D][]Code[C]{}
			} else {
				next.shards[i] = maps.Clone(next.shards[i])
			}
		}
		return next.shards[i]
	}
	for _, d := range changed {
		delete(shard(d), d)
	}
	for d, cs := range fresh {
		shard(d)[d] = cs
	}
	return next
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
		for _, docs := range x.shards {
			for _, cs := range docs {
				for _, c := range cs {
					if !yield(c) {
						return
					}
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

// NearestSeq is Nearest's order, made as it is taken: the codes are scored and put in a heap at
// once, and each chunk taken costs a pop. A caller that takes the first m of N pays O(N + m log N)
// rather than a sort of all N; TwoStageSeq takes only the windows it needs. Taking all of it yields
// what Nearest returns. The sequence can be ranged over once.
func NearestSeq[C any](codes iter.Seq[Code[C]], q []uint64, cmpChunk func(a, b C) int) iter.Seq[C] {
	type scored struct {
		chunk C
		dist  int
	}
	var h []scored
	for c := range codes {
		h = append(h, scored{c.Chunk, Hamming(c.Bits, q)})
	}
	less := func(i, j int) bool {
		if h[i].dist != h[j].dist {
			return h[i].dist < h[j].dist
		}
		return cmpChunk(h[i].chunk, h[j].chunk) < 0
	}
	down := func(i, n int) {
		for {
			l := 2*i + 1
			if l >= n {
				return
			}
			m := l
			if r := l + 1; r < n && less(r, l) {
				m = r
			}
			if !less(m, i) {
				return
			}
			h[i], h[m] = h[m], h[i]
			i = m
		}
	}
	for i := len(h)/2 - 1; i >= 0; i-- {
		down(i, len(h))
	}
	return func(yield func(C) bool) {
		for n := len(h); n > 0; n-- {
			top := h[0].chunk
			h[0] = h[n-1]
			down(0, n-1)
			if !yield(top) {
				return
			}
		}
	}
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
	return TwoStageSeq(slices.Values(order), q, window, n, fetch, less)
}

// TwoStageSeq is TwoStage over an order taken as it is made (NearestSeq): it takes the order's keys
// only as far as the windows it needs, so the rest is never ordered.
func TwoStageSeq[C, T any](order iter.Seq[C], q []float32, window, n int, fetch func(ids []C) ([]Vec[T], error), less func(a, b T) bool) ([]Vec[T], error) {
	window = max(window, 1)
	var valid []Vec[T]
	ids := make([]C, 0, window)
	// take fetches one window; it reports whether the scan goes on
	take := func() (bool, error) {
		got, err := fetch(ids)
		ids = make([]C, 0, window) // fetch may keep the window it was handed, as it could keep TwoStage's
		if errors.Is(err, SkipRest) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		for _, v := range got {
			v.Dot = Dot(q, v.F32)
			valid = append(valid, v)
		}
		return len(valid) < n, nil
	}
	more := n > 0
	if more {
		for c := range order {
			ids = append(ids, c)
			if len(ids) < window {
				continue
			}
			var err error
			if more, err = take(); err != nil {
				return nil, err
			}
			if !more {
				break
			}
		}
		if more && len(ids) > 0 {
			if _, err := take(); err != nil {
				return nil, err
			}
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
